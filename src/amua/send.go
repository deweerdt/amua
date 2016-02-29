package main

import (
	"net/mail"

	"amua/config"
)

// Holds values while a new email is being edited
type NewMail struct {
	to      []*mail.Address
	cc      []*mail.Address
	bcc     []*mail.Address
	subject string
	body    []byte
}

func send(nm *NewMail, smtcfg config.SMTPConfig) error {
	return nil
}
