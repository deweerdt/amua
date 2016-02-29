package main

import (
	"net/mail"
)

// Holds values while a new email is being edited
type NewMail struct {
	to      []*mail.Address
	cc      []*mail.Address
	bcc     []*mail.Address
	subject string
	body    []byte
}

type AuthConfig struct {
	user   string
	passwd string
}

func send(to string, cc string, bcc string, body []byte, auth AuthConfig) error {
	return nil
}
