package drivers

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/TechAlkurn/core/config"

	"github.com/TechAlkurn/core/lib"
	mailjet "github.com/mailjet/mailjet-apiv3-go"
)

func (em *Sender) Mailjet(subject *bytes.Buffer, body *bytes.Buffer, mailer map[string]any) (bool, error) {
	/*
		cnf := components.NewConfig()
		mailjet_api_key := fmt.Sprintf("%v", cnf.Get("mailjet_api_key"))
		mailjet_secret_key := fmt.Sprintf("%v", cnf.Get("mailjet_secret_key"))
		if mailjet_api_key == "" {
			return false, errors.New(cons.MailjetApiKeyBlank)
		}
		if mailjet_secret_key == "" {
			return false, errors.New(cons.MailjetSecretKeyBlank)
		}
	*/

	mailjet_api_key := os.Getenv("MJ_APIKEY_PUBLIC")
	mailjet_secret_key := os.Getenv("MJ_APIKEY_PRIVATE")
	var attachmentsV31 mailjet.AttachmentsV31
	attachments, ok := mailer["attachments"].([]map[string]string)
	if ok && len(attachments) > 0 {
		for _, attachment := range attachments {
			if attachment != nil {
				fileData, err := os.ReadFile(attachment["path"])
				if err != nil {
					fmt.Println(err)
				}
				encoded := base64.StdEncoding.EncodeToString([]byte(fileData))
				attachmentsV31 = append(attachmentsV31, mailjet.AttachmentV31{
					ContentType:   attachment["mimetype"],
					Filename:      attachment["filename"],
					Base64Content: encoded,
				})
			}
		}
	}

	// config.Log.Printf("%v %v\n", mailjet_api_key, mailjet_secret_key)
	// config.Log.Printf("%v %v\n", em.FromEmail, em.FromName)

	plainText := lib.HTMLToText(body.String())
	mj := mailjet.NewMailjetClient(mailjet_api_key, mailjet_secret_key)
	info := []mailjet.InfoMessagesV31{
		{
			From: &mailjet.RecipientV31{Email: em.FromEmail, Name: em.FromName},
			To: &mailjet.RecipientsV31{
				mailjet.RecipientV31{Email: em.To, Name: "_"},
			},
			Subject:     subject.String(),
			TextPart:    plainText,
			HTMLPart:    body.String(),
			Attachments: &attachmentsV31,
		},
	}

	ismail, err := mj.SendMailV31(&mailjet.MessagesV31{Info: info})
	if err != nil {
		config.Log.Printf("Could not send email: %v", err)
		return false, nil
	}
	config.Log.Printf("ismail: %v\n", ismail)
	return true, nil
}
