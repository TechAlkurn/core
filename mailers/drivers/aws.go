package drivers

import (
	"bytes"
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"gopkg.in/gomail.v2"
)

type Recipient struct {
	toEmails  []string
	ccEmails  []string
	bccEmails []string
}

func (em *Sender) SendSESEmail(subject *bytes.Buffer, body *bytes.Buffer, mailer map[string]any) (bool, error) {
	recipient := Recipient{
		toEmails: []string{em.To},
		// ccEmails:  []string{"cc1@example.com"},
		// bccEmails: []string{"bcc1@example.com"},
	}

	// Load AWS config (region configurable)
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("ap-south-1"),
	)
	if err != nil {
		log.Printf("Error loading AWS config: %v\n", err)
		return false, err
	}

	// Build MIME email using gomail
	msg := gomail.NewMessage()
	msg.SetAddressHeader("From", em.FromEmail, em.FromName)
	msg.SetHeader("To", recipient.toEmails...)
	if len(recipient.ccEmails) > 0 {
		msg.SetHeader("Cc", recipient.ccEmails...)
	}
	if len(recipient.bccEmails) > 0 {
		msg.SetHeader("Bcc", recipient.bccEmails...)
	}
	msg.SetHeader("Subject", subject.String())
	msg.SetBody("text/html", body.String())

	// Add attachments if provided
	if attachments, ok := mailer["attachments"].([]map[string]string); ok {
		for _, f := range attachments {
			if filename, ok := f["filename"]; ok && filename != "" {
				msg.Attach(filename)
			}
		}
	}

	// Write MIME to buffer
	var emailRaw bytes.Buffer
	if _, err := msg.WriteTo(&emailRaw); err != nil {
		log.Println("Error writing MIME message:", err)
		return false, err
	}

	// Send email via SES
	input := &ses.SendRawEmailInput{
		RawMessage: &types.RawMessage{
			Data: emailRaw.Bytes(),
		},
	}

	svc := ses.NewFromConfig(cfg)
	_, err = svc.SendRawEmail(context.TODO(), input)
	if err != nil {
		log.Println("Error sending mail:", err)
		return false, err
	}

	log.Println("Email sent successfully to:", em.To)
	return true, nil
}
