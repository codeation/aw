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

var errNotFound = errors.New("Record not found")

type cfRecordRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

// request parses the CloudFlare response
func (cf *cfAccount) request(method, url string, body interface{}, v interface{}) error {
	client := &http.Client{}
	bodyData, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if body == nil {
		bodyData = nil
	}
	url = "https://api.cloudflare.com/client/v4" + url
	req, err := http.NewRequest(method, url, bytes.NewReader(bodyData))
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
			return errors.New("Set record " + name + " to " + ip + " error, still " + record.Result.Content)
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
			return errors.New("Set record " + name + " to " + ip + " error, still " + record.Result.Content)
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
	if cf.zoneID != "" {
		return nil
	}
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
		return errors.New("Unknown CF format")
	}
	cf.zoneID = zone.Result[0].ID
	return nil
}

func (cf *cfAccount) moveRecords(sourceIP, targetIP string) error {
	if err := cf.loadZone(); err != nil {
		return err
	}
	records, err := cf.loadRecords(cf.names, "A")
	if err != nil {
		return err
	}
	if sourceIP != "" && !isAddrEqual(records["@"].content, sourceIP) {
		return errors.New("Stated IP is " + records["@"].content)
	}
	if time.Since(records["@"].modified) < 10*time.Minute {
		return errors.New("Record updated recently")
	}
	return cf.setRecords(targetIP, "A", records)
}

func (cf *cfAccount) moveRecordsIPv6(sourceIPv6, targetIPv6 string) error {
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
				return errors.New("Record updated recently")
			}
			return cf.setRecords(targetIPv6, "AAAA", records)
		}
		// else delete
		return cf.deleteRecords("AAAA", records)
	}
	return nil
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
