package mailers

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"strings"

	"github.com/TechAlkurn/core/cons"
	"github.com/TechAlkurn/core/mailers/drivers"

	"github.com/TechAlkurn/core/config"

	"gorm.io/gorm"
)

// Template is an already-resolved (and already-translated) mail template.
// The caller owns lookup/translation; this package only renders and sends.
type Template struct {
	Key     string // template key, shown in the non-live footer
	Subject string // html/template source for the subject
	Content string // html/template source for the body
	Layout  string // html/template source for the surrounding layout
}

type Mailer struct {
	To          string
	Template    *Template
	Mailer      map[string]any
	Config      map[string]any
	Attachments any
	Delay       int

	FromEmail    string
	FromName     string
	SmtpPassword string
	SmtpUsername string
	SmtpHost     string
	SmtpPort     int
}

const content = `{{.content}}
{{ if ne .isLive true}}
    <p style="margin-bottom: 0px;"><small style="font-size:9px;float: right" class="text-info">TemplateKey : {{.key}}</small></p>
{{end}}`

type EmailManager interface {
	Mail() (bool, error)
}

// NewMailer renders tpl with the mailer payload and sends it to `to`.
func NewMailer(to string, tpl *Template, mailer map[string]any, cnf map[string]any, delay int) EmailManager {
	if mailer == nil {
		mailer = map[string]any{}
	}
	return &Mailer{To: to, Template: tpl, Mailer: mailer, Config: cnf, Delay: delay}
}

// Under testing Gomail,Mailjet,Sendgrid
func (em *Mailer) Mail() (bool, error) {
	tpl := em.Template
	mailer := em.Mailer
	if tpl == nil {
		return false, errors.New(cons.MailTemplateNotLoaded)
	}
	if tpl.Layout == "" {
		return false, errors.New(cons.MailLayoutNotFound)
	}
	em.FromEmail = config.GetString("from_email")
	em.FromName = config.GetString("from_name")
	if em.FromEmail == "" || em.FromName == "" {
		return false, errors.New(cons.FromEmailOrNameBlank)
	}
	from_domain := config.GetString("from_domain")
	alias_domain := config.GetString("alias_domain")
	if from_domain != "" || alias_domain != "" {
		em.To = strings.Replace(em.To, fmt.Sprintf("@%s", from_domain), fmt.Sprintf("@%s", alias_domain), 1)
	}
	em.SmtpPassword = config.GetString("smtp_password")
	em.SmtpUsername = config.GetString("smtp_username")
	em.SmtpHost = config.GetString("smtp_host")
	em.SmtpPort = config.GetInt("smtp_port")
	site_mode := config.GetString("site_mode")
	support_team_title := config.GetString("footer_mail_support_team_title")

	mailer["key"] = tpl.Key
	mailer["isLive"] = site_mode == "production" || site_mode == "Production"
	mailer["site_path"] = config.GetString("site_path")
	mailer["site_logo"] = config.GetString("site_logo")
	mailer["site_name"] = config.GetString("site_name")
	mailer["support_team_title"] = template.HTML(support_team_title)

	tmplSubject, err := template.New("subject").Parse(tpl.Subject)
	if err != nil {
		return false, err
	}
	var subject bytes.Buffer
	if err = tmplSubject.Execute(&subject, mailer); err != nil {
		return false, err
	}
	mailer["subject"] = subject.String()
	tmpl, err := template.New("modelContent").Parse(tpl.Content)
	if err != nil {
		return false, err
	}
	var contentBody bytes.Buffer
	if err = tmpl.Execute(&contentBody, mailer); err != nil {
		return false, err
	}
	mailer["content"] = template.HTML(contentBody.String())
	var htmlContentbody bytes.Buffer
	content := template.Must(template.New("content").Parse(content))
	_ = content.Execute(&htmlContentbody, mailer)
	mailer["content"] = template.HTML(htmlContentbody.String())
	var body bytes.Buffer

	layout, err := template.New("htmlContent").Parse(tpl.Layout)
	if err != nil {
		return false, err
	}
	if err = layout.Execute(&body, mailer); err != nil {
		return false, err
	}

	// return em.Gomail(&subject, &body, mailer)
	// return em.Sendgrid(&subject, &body, mailer)
	// mailerType := config.GetString("mailer_type"))

	sender := &drivers.Sender{
		To:           em.To,
		FromEmail:    em.FromEmail,
		FromName:     em.FromName,
		SmtpHost:     em.SmtpHost,
		SmtpPort:     em.SmtpPort,
		SmtpUsername: em.SmtpUsername,
		SmtpPassword: em.SmtpPassword,
	}

	mailerType := "sendgrid"
	isMail := false
	switch mailerType {
	case "sendgrid":
		isMail, err = sender.SendGrid(&subject, &body, mailer)
	case "mailjet":
		isMail, err = sender.Mailjet(&subject, &body, mailer)
	default: // "gomail"
		isMail, err = sender.Gomail(&subject, &body, mailer)
	}
	if !isMail {
		em._inQueue(mailer, cons.CORE_MAIL_STATUS_NO)
	}
	//	config.Log.Printf("EmailId: %v isMail: %v\n", em.To, isMail)
	return isMail, err
}

func (em *Mailer) _inQueue(mailer map[string]any, status int) bool {
	subject := fmt.Sprint(mailer["subject"])
	message := fmt.Sprint(mailer["content"])

	var recipients []string
	for _, email := range strings.Split(em.To, ",") {
		if email = strings.TrimSpace(email); email != "" {
			recipients = append(recipients, email)
		}
	}
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			"INSERT INTO `core_mail` (`subject`, `message`, `status`, `created_at`, `updated_at`) VALUES (?, ?, ?, UNIX_TIMESTAMP(), UNIX_TIMESTAMP())",
			subject, message, status,
		).Error; err != nil {
			return err
		}
		var mailId uint64
		if err := tx.Raw("SELECT LAST_INSERT_ID()").Scan(&mailId).Error; err != nil {
			return err
		}
		if len(recipients) == 0 {
			return nil
		}
		placeholders := make([]string, len(recipients))
		args := make([]any, 0, len(recipients)*2)
		for i, email := range recipients {
			placeholders[i] = "(?, ?)"
			args = append(args, mailId, email)
		}
		return tx.Exec(
			"INSERT INTO `core_mail_recipients` (`mail_id`, `email`) VALUES "+strings.Join(placeholders, ", "),
			args...,
		).Error
	})
	if err != nil {
		config.Log.Printf("mail queue insert failed for %s: %v\n", em.To, err)
		return false
	}
	return true
}
