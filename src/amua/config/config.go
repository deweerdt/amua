package config

import (
	"bufio"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Maildirs []string
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
