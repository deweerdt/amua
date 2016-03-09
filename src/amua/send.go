package main

import (
	"crypto/tls"
	"net/mail"
	"net/smtp"

	"amua/config"
	"amua/util"
)

// Holds values while a new email is being edited
type NewMail struct {
	to      []*mail.Address
	cc      []*mail.Address
	bcc     []*mail.Address
	subject string
	body    []byte
}

func sendMail(addr, hello, tlsServerName string, a smtp.Auth, from string, to []string, msg []byte) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.Hello(hello); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		config := &tls.Config{ServerName: tlsServerName}
		if err = c.StartTLS(config); err != nil {
			return err
		}
	}
	if a != nil {
		if err = c.Auth(a); err != nil {
			return err
		}
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err = c.Rcpt(addr); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(msg)
	if err != nil {
		return err
	}
	err = w.Close()
	if err != nil {
		return err
	}
	return c.Quit()
}

func send(nm *NewMail, cfg *config.Config) error {
	rcpts := append(util.AddressesToString(nm.to), util.AddressesToString(nm.cc)...)
	rcpts = append(rcpts, util.AddressesToString(nm.bcc)...)
	return sendMail(cfg.SMTPConfig.Host, "localhost", "", nil, cfg.AmuaConfig.Me, rcpts, nm.body)
}
