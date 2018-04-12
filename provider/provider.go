package provider

import (
	"fmt"
	"io"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/mulesoft-labs/aws-keycloak/provider/saml"
)

// Package level vars
var (
	ProviderIn  io.Reader = os.Stdin
	ProviderOut io.Writer = os.Stdout
	ProviderErr io.Writer = os.Stderr
)

type Provider struct {
	K KeycloakProviderIf
	A AwsProviderIf
}

func (p *Provider) Retrieve(awsrole string) (sts.Credentials, string, error) {
	log.Debug("Step 0: Checking existing AWS session")
	creds, err := p.A.CheckAlreadyAuthd(awsrole)
	if err == nil {
		log.Debugf("AWS session already valid for %s", awsrole)
		return creds, awsrole, nil
	}

	log.Debug("Step 1: Auth to Keycloak")
	err = p.K.BrowserAuth()
	/** Basic auth is deprecated
	newCreds := p.K.RetrieveKeycloakCreds()
	err = p.K.BasicAuth()
	*/
	if err != nil {
		return sts.Credentials{}, "", fmt.Errorf("Failed to authenticate with keycloak: %s", err)
	}

	log.Debug("Step 2: Get SAML form Keycloak")
	assertion, err := p.K.GetSamlAssertion()
	if err != nil {
		return sts.Credentials{}, "", err
	}

	rps, _, err := saml.GetRolesFromSAML(assertion.Resp)
	if err != nil {
		return sts.Credentials{}, "", err
	}
	awsshortrole, n := PromptMultiMatchRole(saml.RolesOf(rps), awsrole)

	log.Debug("Step 3: Use SAML to assume AWS role")
	log.Infof("Assuming role '%s'", awsshortrole)
	log.Infof("  You can specify this role with the --profile flag if you also put it in your aws config.")
	log.Infof("  Hint: use `aws configure --profile %s` and don't enter any Key ID or Secret Key.", awsshortrole)
	creds, err = p.A.AssumeRoleWithSAML(rps[n], string(assertion.RawResp))
	if err != nil {
		if err.(awserr.Error).Code() == sts.ErrCodeExpiredTokenException {
			log.Errorf("You took too long to pick a role")
		} else {
			log.Errorf("Error assuming role with SAML")
		}
		return sts.Credentials{}, "", err
	} else {
		log.WithField("role", awsshortrole).Info("Successfully assumed role with SAML")
	}

	/** Used when doing BasicAuth
	// Save keycloak creds since auth was successful
	if newCreds {
		p.K.StoreKeycloakCreds()
	}
	*/

	p.A.StoreAwsCreds(creds, awsshortrole)

	return creds, awsshortrole, err
}
