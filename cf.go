package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/codeation/inifile"
)

// Zone record
type cfRecord struct {
	id      string
	content string
}

// CloudFlare account
type cfAccount struct {
	email   string
	apiKey  string
	domain  string
	zoneID  string
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
				ID      string
				Content string
			}
		}
		if err := cf.request("GET", url, nil, &record); err != nil {
			return err
		}
		if len(record.Result) != 1 {
			return errors.New("Unknown CF format")
		}
		cf.records[name] = cfRecord{
			id:      record.Result[0].ID,
			content: record.Result[0].Content,
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
			return errors.New("Set record " + name + " to " + ip + " error, remain " + record.Result.Content)
		}
		cf.records[name] = cfRecord{
			id:      cf.records[name].id,
			content: record.Result.Content,
		}
	}
	return nil
}

// newAccount saves account credentials and reads zone ID and zone records
func newAccount(ini *inifile.IniFile) (*cfAccount, error) {
	cf := &cfAccount{
		email:  ini.Get("", "email"),
		apiKey: ini.Get("", "apikey"),
		domain: ini.Get("", "domain"),
	}
	names := strings.Split(ini.Get("", "names"), ",")

	url := "https://api.cloudflare.com/client/v4/zones?name=" + cf.domain
	var zone struct {
		Result []struct {
			ID string
		}
	}
	if err := cf.request("GET", url, nil, &zone); err != nil {
		return nil, err
	}
	if len(zone.Result) != 1 {
		return nil, errors.New("Unknown CF format")
	}
	cf.zoneID = zone.Result[0].ID

	if err := cf.loadRecords(names); err != nil {
		return nil, err
	}

	return cf, nil
}
