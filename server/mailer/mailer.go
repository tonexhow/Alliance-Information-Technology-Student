package mailer

import (
	"bytes"
	"crypto/tls"
	"embed"
	"fmt"
	"html/template"
	"net"
	"net/smtp"
	"strings"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

type Config struct{ Email, Password, Host, Port string }
type Client struct {
	config      Config
	templates   *template.Template
	templateErr error
}

type TemplateData struct {
	Title       string
	Preheader   string
	Greeting    string
	Message     string
	Code        string
	ActionLabel string
	ActionURL   string
	DetailLabel string
	DetailValue string
	Footer      string
}

func New(config Config) Client {
	t, err := template.ParseFS(templateFS, "templates/*.html")
	return Client{config: config, templates: t, templateErr: err}
}
func (c Client) Ready() bool {
	return strings.TrimSpace(c.config.Email) != "" && strings.TrimSpace(c.config.Password) != ""
}

func (c Client) Send(to, subject, body string) error {
	return c.SendTemplate(to, subject, "generic.html", TemplateData{Title: subject, Message: body, Footer: "Alliance of Information Technology Students · Buenavista Community College"})
}
func (c Client) SendVerification(to, greeting, code, purpose string) error {
	title := "Verify your AITS account"
	message := "Use the verification code below to continue. The code expires in 15 minutes."
	if purpose == "email_change" {
		title = "Verify your new AITS email"
		message = "Confirm that this email belongs to you before it is connected to your AITS member account."
	}
	return c.SendTemplate(to, title, "verification.html", TemplateData{Title: title, Preheader: "Your AITS verification code", Greeting: greeting, Message: message, Code: code, Footer: "If you did not request this change, you can safely ignore this email."})
}
func (c Client) SendApplication(to, name, applicationNumber, publicURL string) error {
	return c.SendTemplate(to, "AITS application received", "application.html", TemplateData{Title: "Application received", Preheader: "Your AITS application has been recorded", Greeting: name, Message: "Your application has been received. Keep your application number for future reference and watch your inbox for the next update.", DetailLabel: "Application number", DetailValue: applicationNumber, ActionLabel: "Open AITS", ActionURL: strings.TrimRight(publicURL, "/") + "/apply", Footer: "Alliance of Information Technology Students · Buenavista Community College"})
}
func (c Client) SendMemberInvitation(to, name, actionURL string) error {
	return c.SendTemplate(to, "AITS membership invitation", "generic.html", TemplateData{Title: "You are invited to join AITS", Preheader: "Accept your AITS membership within 48 hours", Greeting: name, Message: "AITS Administration prepared a membership invitation for you. Open the secure one-time link below to review the account details and accept membership. The link expires 48 hours after it was issued and can only be used once.", ActionLabel: "BE A MEMBER", ActionURL: actionURL, Footer: "If you did not expect this invitation, contact AITS Administration."})
}
func (c Client) SendPasswordReset(to, name, actionURL string) error {
	return c.SendTemplate(to, "Reset your AITS password", "generic.html", TemplateData{Title: "Reset your AITS password", Preheader: "A one-time password reset was requested by AITS Administration", Greeting: name, Message: "An authorized AITS administrator requested a password reset for your member account. Use the secure one-time button below to choose a new password. The link expires in 15 minutes and stops working immediately after it is used.", ActionLabel: "RESET PASSWORD", ActionURL: actionURL, Footer: "If you did not expect this reset, do not use the link and contact AITS Administration."})
}

func (c Client) SendTemplate(to, subject, name string, data TemplateData) error {
	if !c.Ready() {
		return fmt.Errorf("SMTP is not configured")
	}
	if c.templateErr != nil {
		return fmt.Errorf("load email templates: %w", c.templateErr)
	}
	if c.templates == nil {
		return fmt.Errorf("email templates are unavailable")
	}
	var html bytes.Buffer
	if err := c.templates.ExecuteTemplate(&html, name, data); err != nil {
		return err
	}
	return c.sendHTML(to, subject, html.String())
}

func (c Client) sendHTML(to, subject, body string) error {
	addr := net.JoinHostPort(c.config.Host, c.config.Port)
	conn, err := net.DialTimeout("tcp", addr, 12*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, c.config.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: c.config.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if err := client.Auth(smtp.PlainAuth("", c.config.Email, c.config.Password, c.config.Host)); err != nil {
		return err
	}
	if err := client.Mail(c.config.Email); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	message := strings.Join([]string{"From: AITS <" + c.config.Email + ">", "To: " + to, "Subject: " + subject, "MIME-Version: 1.0", "Content-Type: text/html; charset=UTF-8", "Content-Transfer-Encoding: 8bit", "", body}, "\r\n")
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}
