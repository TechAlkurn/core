package drivers

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/TechAlkurn/core/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"gopkg.in/gomail.v2"
)

func (em *Sender) Gomail(subject *bytes.Buffer, body *bytes.Buffer, mailer map[string]any) (bool, error) {
	mail := gomail.NewMessage()

	if strings.TrimSpace(em.FromEmail) == "" {
		return false, fmt.Errorf("from email address is empty")
	}
	if strings.TrimSpace(em.To) == "" {
		return false, fmt.Errorf("recipient email address is empty")
	}

	// VerifyEmailIdentity(em.FromEmail)
	// VerifyEmailIdentity(em.To)

	mail.SetAddressHeader("From", em.FromEmail, em.FromName)
	toList := filterEmails(em.To)
	if len(toList) == 0 {
		return false, fmt.Errorf("no valid recipient emails")
	}
	mail.SetHeader("To", toList...)

	mail.SetHeader("Subject", subject.String())
	mail.SetBody("text/html", body.String())
	attachments, ok := mailer["attachments"].([]map[string]string)
	if ok && len(attachments) > 0 {
		for _, attachment := range attachments {
			if attachment != nil {
				if attachment["path"] != "" {
					mail.Attach(attachment["path"])
				}
			}
		}
	}

	config.Log.Printf("em.To: %v\n", em.To)
	config.Log.Printf("em.FromEmail: %v\n", em.FromEmail)
	config.Log.Printf("em.SmtpHost: %v\n", em.SmtpHost)
	config.Log.Printf("em.SmtpPort: %v\n", em.SmtpPort)
	config.Log.Printf("em.SmtpUsername: %v\n", em.SmtpUsername)

	dialer := gomail.NewDialer(em.SmtpHost, em.SmtpPort, em.SmtpUsername, em.SmtpPassword)
	// Send Email
	if err := dialer.DialAndSend(mail); err != nil {
		config.Log.Printf("Could not send email: %v", err)
		return false, err
	}
	return true, nil
}

func VerifyEmailIdentity(email string) bool {
	// Load AWS config
	cfg, err := awsconfig.LoadDefaultConfig(
		context.TODO(),
		awsconfig.WithRegion("ap-southeast-1"),
	)
	if err != nil {
		config.Log.Printf("unable to load SDK config, %v", err)
		return false
	}
	client := ses.NewFromConfig(cfg)
	_, _ = client.VerifyEmailIdentity(context.TODO(), &ses.VerifyEmailIdentityInput{
		EmailAddress: aws.String(email),
	})
	return true
}

func filterEmails(emails ...string) []string {
	var valid []string
	for _, e := range emails {
		e = strings.TrimSpace(e)
		if e != "" {
			valid = append(valid, e)
		}
	}
	return valid
}
