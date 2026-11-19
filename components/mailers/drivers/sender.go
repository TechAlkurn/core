package drivers

// Sender carries everything a mail driver needs to deliver one message.
// It is built by mailers.Mailer; drivers must not import the mailers package
// (that would be an import cycle).
type Sender struct {
	To        string
	FromEmail string
	FromName  string

	SmtpHost     string
	SmtpPort     int
	SmtpUsername string
	SmtpPassword string
}
