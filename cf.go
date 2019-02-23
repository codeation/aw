package main

import (
	"bytes"
	"encoding/json"
	"errors"
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
	email  string
	apiKey string
	domain string
	zoneID string
	names  []string
}

// CloudFlare config
type cfConfig struct {
	ini *inifile.IniFile
}

var errNotFound = errors.New("record not found")

type cfRecordRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

// request parses the CloudFlare response
func (cf *cfAccount) request(method, url string, body interface{}, v interface{}) error {
	client := &http.Client{}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if body == nil {
		reqBody = nil
	}
	url = "https://api.cloudflare.com/client/v4" + url
	req, err := http.NewRequest(method, url, bytes.NewReader(reqBody))
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
func (cf *cfAccount) loadRecords(names []string, recordType string) (map[string]cfRecord, error) {
	records := map[string]cfRecord{}
	for _, name := range names {
		fullname := name + "." + cf.domain
		if name == "@" {
			fullname = cf.domain
		}
		url := "/zones/" + cf.zoneID + "/dns_records" +
			"?type=" + recordType + "&name=" + fullname + "&match=all"
		var record struct {
			Result []struct {
				ID       string
				Content  string
				Modified string `json:"modified_on"`
			}
		}
		if err := cf.request("GET", url, nil, &record); err != nil {
			return nil, err
		}
		if len(record.Result) == 0 {
			return nil, errNotFound
		}
		modified, err := time.Parse(time.RFC3339, record.Result[0].Modified)
		if err != nil {
			return nil, err
		}
		records[name] = cfRecord{
			id:       record.Result[0].ID,
			content:  record.Result[0].Content,
			modified: modified,
		}
	}
	return records, nil
}

// setRecords changes previosly loaded zone records to a new IP
func (cf *cfAccount) setRecords(ip string, recordType string, records map[string]cfRecord) error {
	for name, r := range records {
		fullname := name + "." + cf.domain
		if name == "@" {
			fullname = cf.domain
		}
		url := "/zones/" + cf.zoneID + "/dns_records/" + r.id
		body := &cfRecordRequest{
			Type:    recordType,
			Name:    fullname,
			Content: ip,
			Proxied: false,
		}
		var record struct {
			Result struct {
				Content string
			}
		}
		if err := cf.request("PUT", url, body, &record); err != nil {
			return err
		}
		if !isAddrEqual(record.Result.Content, ip) {
			return errors.New("set record " + name + " to " + ip + " error, still " + record.Result.Content)
		}
	}
	return nil
}

// createRecords creates zone records
func (cf *cfAccount) createRecords(ip string, recordType string, names []string) error {
	for _, name := range names {
		fullname := name + "." + cf.domain
		if name == "@" {
			fullname = cf.domain
		}
		url := "/zones/" + cf.zoneID + "/dns_records"
		body := &cfRecordRequest{
			Type:    recordType,
			Name:    fullname,
			Content: ip,
			Proxied: false,
		}
		var record struct {
			Result struct {
				Content string
			}
		}
		if err := cf.request("POST", url, body, &record); err != nil {
			return err
		}
		if !isAddrEqual(record.Result.Content, ip) {
			return errors.New("set record " + name + " to " + ip + " error, still " + record.Result.Content)
		}
	}
	return nil
}

// deleteRecords deletes zone records
func (cf *cfAccount) deleteRecords(recordType string, records map[string]cfRecord) error {
	for _, r := range records {
		url := "/zones/" + cf.zoneID + "/dns_records/" + r.id
		var record struct{}
		if err := cf.request("DELETE", url, nil, &record); err != nil {
			return err
		}
	}
	return nil
}

// loadZone reads zone ID
func (cf *cfAccount) loadZone() error {
	url := "/zones?name=" + cf.domain
	var zone struct {
		Result []struct {
			ID string
		}
	}
	if err := cf.request("GET", url, nil, &zone); err != nil {
		return err
	}
	if len(zone.Result) != 1 {
		return errors.New("unknown CF format")
	}
	cf.zoneID = zone.Result[0].ID
	return nil
}

// newAccount saves account credentials and reads zone ID and zone records
func (c *cfConfig) newAccount() *cfAccount {
	return &cfAccount{
		email:  c.ini.Get("", "email"),
		apiKey: c.ini.Get("", "apikey"),
		domain: c.ini.Get("", "domain"),
		names:  strings.Split(c.ini.Get("", "names"), ","),
	}
}

// moveRecords changes specified A records from sourceIP to targetIP
func (c *cfConfig) moveRecords(sourceIP, targetIP string) error {
	cf := c.newAccount()
	if err := cf.loadZone(); err != nil {
		return err
	}
	records, err := cf.loadRecords(cf.names, "A")
	if err != nil {
		return err
	}
	if sourceIP != "" && !isAddrEqual(records["@"].content, sourceIP) {
		return errors.New("stated IP is " + records["@"].content)
	}
	if time.Since(records["@"].modified) < 10*time.Minute {
		return errors.New("record updated recently")
	}
	return cf.setRecords(targetIP, "A", records)
}

// moveRecordsIPv6 changes specified AAAA records from sourceIPv6 to targetIPv6
func (c *cfConfig) moveRecordsIPv6(sourceIPv6, targetIPv6 string) error {
	cf := c.newAccount()
	if err := cf.loadZone(); err != nil {
		return err
	}
	records, err := cf.loadRecords(cf.names, "AAAA")
	if err != nil && err != errNotFound {
		return err
	}
	if err == errNotFound {
		// no any records detected
		if targetIPv6 != "" {
			return cf.createRecords(targetIPv6, "AAAA", cf.names)
		}
		// else source and targets are blank
	} else {
		// records detected
		if targetIPv6 != "" {
			// update
			if time.Since(records["@"].modified) < 10*time.Minute {
				return errors.New("record updated recently")
			}
			return cf.setRecords(targetIPv6, "AAAA", records)
		}
		// else delete
		return cf.deleteRecords("AAAA", records)
	}
	return nil
}

func newCFConfig(ini *inifile.IniFile) *cfConfig {
	return &cfConfig{
		ini: ini,
	}
}
