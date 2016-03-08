package config

import (
	"bufio"
	"os"

	"github.com/BurntSushi/toml"
)

type SMTPConfig struct {
	Host   string
	User   string
	Passwd string
}

type AmuaConfig struct {
	Maildirs []string
	Me       []string
	Editor   string
}
type Config struct {
	AmuaConfig AmuaConfig
	SMTPConfig SMTPConfig
}

func NewConfig(filename string) (*Config, error) {
	cfg := &Config{}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := toml.DecodeReader(bufio.NewReader(file), &cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
