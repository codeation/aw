package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/codeation/inifile"
)

// Zone record
type cfRecord struct {
	id       string
	content  string
	modified time.Time
}

// CloudFlare account
type cfAccount struct {
	email   string
	apiKey  string
	domain  string
	zoneID  string
	names   []string
	records map[string]cfRecord
}

// request parses the CloudFlare response
func (cf *cfAccount) request(method, url string, body io.Reader, v interface{}) error {
	client := &http.Client{}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return err
	}
	req.Header.Add("X-Auth-Email", cf.email)
	req.Header.Add("X-Auth-Key", cf.apiKey)
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(http.StatusText(resp.StatusCode))
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// loadRecords reads zone records
func (cf *cfAccount) loadRecords(names []string) error {
	cf.records = map[string]cfRecord{}
	for _, name := range names {
		fullname := name + "." + cf.domain
		if name == "@" {
			fullname = cf.domain
		}
		url := "https://api.cloudflare.com/client/v4/zones/" + cf.zoneID +
			"/dns_records?type=A&name=" + fullname + "&match=all"
		var record struct {
			Result []struct {
				ID       string
				Content  string
				Modified string `json:"modified_on"`
			}
		}
		if err := cf.request("GET", url, nil, &record); err != nil {
			return err
		}
		if len(record.Result) != 1 {
			return errors.New("Unknown CF format")
		}
		modified, err := time.Parse(time.RFC3339, record.Result[0].Modified)
		if err != nil {
			return err
		}
		cf.records[name] = cfRecord{
			id:       record.Result[0].ID,
			content:  record.Result[0].Content,
			modified: modified,
		}

	}
	return nil
}

// setRecords changes previosly loaded zone records to a new IP
func (cf *cfAccount) setRecords(ip string) error {
	for name := range cf.records {
		fullname := name + "." + cf.domain
		if name == "@" {
			fullname = cf.domain
		}
		url := "https://api.cloudflare.com/client/v4/zones/" + cf.zoneID +
			"/dns_records/" + cf.records[name].id
		body := `{"type":"A","name":"` + fullname + `","content":"` + ip + `","proxied":false}`
		var record struct {
			Result struct {
				Content string
			}
		}
		if err := cf.request("PUT", url, strings.NewReader(body), &record); err != nil {
			return err
		}
		if record.Result.Content != ip {
			return errors.New("Set record " + name + " to " + ip + " error, still " + record.Result.Content)
		}
		cf.records[name] = cfRecord{
			id:      cf.records[name].id,
			content: record.Result.Content,
		}
	}
	return nil
}

// loadZone reads zone ID
func (cf *cfAccount) loadZone() error {
	if cf.zoneID != "" {
		return nil
	}

	url := "https://api.cloudflare.com/client/v4/zones?name=" + cf.domain
	var zone struct {
		Result []struct {
			ID string
		}
	}
	if err := cf.request("GET", url, nil, &zone); err != nil {
		return err
	}
	if len(zone.Result) != 1 {
		return errors.New("Unknown CF format")
	}
	cf.zoneID = zone.Result[0].ID
	return nil
}

func (cf *cfAccount) moveRecords(sourceIP, targetIP string) error {
	if err := cf.loadZone(); err != nil {
		return err
	}

	if err := cf.loadRecords(cf.names); err != nil {
		return err
	}

	if sourceIP != "" && cf.records["@"].content != sourceIP {
		return errors.New("Stated IP is " + cf.records["@"].content)
	}

	if time.Since(cf.records["@"].modified) < 10*time.Minute {
		return errors.New("Record updated recently")
	}

	return cf.setRecords(targetIP)
}

// newAccount saves account credentials and reads zone ID and zone records
func newAccount(ini *inifile.IniFile) (*cfAccount, error) {
	return &cfAccount{
		email:  ini.Get("", "email"),
		apiKey: ini.Get("", "apikey"),
		domain: ini.Get("", "domain"),
		names:  strings.Split(ini.Get("", "names"), ","),
	}, nil
}
