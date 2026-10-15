package main

import (
	"fmt"
	"net/url"
	"time"

	"github.com/BurntSushi/toml"
)

const defaultURL = "https://api3.ablemobile.com/service/smsapi.asmx/SendSMS"

type options struct {
	URL          string `toml:"url"`
	Username     string `toml:"username"`
	Password     string `toml:"password"`
	UserDefineNo string `toml:"user_define_no"`
	Hex          string `toml:"hex"`
	Sender       string `toml:"sender"`
	Timeout      string `toml:"timeout"`
	timeout      time.Duration
}

func decodeOptions(data []byte) (options, error) {
	var o options
	meta, err := toml.Decode(string(data), &o)
	if err != nil {
		return o, fmt.Errorf("AbleMobile options: %w", err)
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		return o, fmt.Errorf("AbleMobile unknown option %q", keys[0])
	}
	if o.Username == "" || o.Password == "" {
		return o, fmt.Errorf("AbleMobile username and password are required")
	}
	if o.URL == "" {
		o.URL = defaultURL
	}
	u, err := url.Parse(o.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return o, fmt.Errorf("AbleMobile url must be HTTP(S)")
	}
	if o.UserDefineNo == "" {
		o.UserDefineNo = "00000"
	}
	o.timeout = 30 * time.Second
	if o.Timeout != "" {
		o.timeout, err = time.ParseDuration(o.Timeout)
		if err != nil || o.timeout <= 0 {
			return o, fmt.Errorf("AbleMobile timeout must be a positive duration")
		}
	}
	return o, nil
}
