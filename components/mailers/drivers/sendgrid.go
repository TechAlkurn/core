package drivers

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/TechAlkurn/core/cons"

	"github.com/TechAlkurn/core/config"

	"github.com/TechAlkurn/core/lib"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

func (em *Sender) SendGrid(subject *bytes.Buffer, body *bytes.Buffer, mailer map[string]any) (bool, error) {
	sendgrid_api_key := config.GetString("sendgrid_api_key")
	// config.Log.Printf("sendgrid_api_key: %v\n", sendgrid_api_key)
	// config.Log.Printf("FromName: %v\n", em.FromName)
	// config.Log.Printf("FromEmail: %v\n", em.FromEmail)
	// config.Log.Printf("To: %v\n", em.To)

	from := mail.NewEmail(em.FromName, em.FromEmail)
	plainText := lib.HTMLToText(body.String())
	to := mail.NewEmail("To", em.To)
	message := mail.NewSingleEmail(from, subject.String(), to, plainText, body.String())
	attachments, ok := mailer["attachments"].([]map[string]string)
	if ok && len(attachments) > 0 {
		for _, attachment := range attachments {
			if attachment != nil {
				file := mail.NewAttachment()
				fileData, err := os.ReadFile(attachment["path"])
				if err != nil {
					fmt.Println(err)
				}
				encoded := base64.StdEncoding.EncodeToString([]byte(fileData))
				file.SetContent(encoded)
				file.SetType(attachment["mimetype"])
				file.SetFilename(attachment["filename"])
				file.SetDisposition("attachment")
				message.AddAttachment(file)
			}
		}
	}
	client := sendgrid.NewSendClient(sendgrid_api_key)
	_, err := client.Send(message)
	if err != nil {
		// 	config.Log.Printf("sendgrid err: %v\n", err)
		return false, err
	}
	// config.Log.Printf("sendgrid err: %v\n", err)
	return true, nil
}

func (em *Sender) TestSendGrid() error {
	sendgrid_api_key := config.GetString("sendgrid_api_key")

	from_email := config.GetString("from_email")
	from_name := config.GetString("from_name")

	if from_email == "" || from_name == "" {
		return errors.New(cons.FromEmailOrNameBlank)
	}

	from := mail.NewEmail(from_name, from_email)
	subject := "Sending with Twilio SendGrid is Fun"
	to := mail.NewEmail("Example User", "mganesh.alkurn@gmail.com")
	plainTextContent := "and easy to do anywhere, even with Go"
	htmlContent := "<strong>and easy to do anywhere, even with Go</strong>"
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
	client := sendgrid.NewSendClient(sendgrid_api_key)
	response, err := client.Send(message)
	if err != nil {
		return err
	} else {
		fmt.Println(response.StatusCode)
		fmt.Println(response.Body)
		fmt.Println(response.Headers)
	}
	return nil
}
