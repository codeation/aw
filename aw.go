package main

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/codeation/inifile"
)

type node struct {
	name string
	ip   string
}

type config struct {
	ttl      time.Duration
	domain   string
	watchURL string
	timeout  time.Duration
	nodes    []node
	cf       *cfAccount
}

func lookupDomain(domain string) (string, error) {
	addrs, err := net.LookupHost(domain)
	if err != nil {
		return "", err
	}
	if len(addrs) < 1 {
		// lookup without errors
		return "", nil
	}
	return addrs[0], nil
}

func parseDuration(value string, defaultValue int, multiplier time.Duration) time.Duration {
	n, err := strconv.Atoi(value)
	if err != nil || n == 0 {
		n = defaultValue
	}
	return time.Duration(n) * multiplier
}

func loadConfig(filename string) (*config, error) {
	ini, err := inifile.Read(filename)
	if err != nil {
		return nil, err
	}
	cfg := &config{
		ttl:      parseDuration(ini.Get("", "ttl"), 60, time.Second),
		domain:   ini.Get("", "domain"),
		watchURL: ini.Get("", "url"),
		timeout:  parseDuration(ini.Get("", "timeout"), 60, time.Second),
	}
	for _, name := range ini.Sections() {
		cfg.nodes = append(cfg.nodes, node{
			name: name,
			ip:   ini.Get(name, "ip"),
		})
	}

	cfg.cf, err = newAccount(ini)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (cfg *config) checkURL() (bool, time.Duration, string) {
	t0 := time.Now()
	remoteIP := ""
	client := &http.Client{
		Timeout: cfg.timeout,
		Transport: &http.Transport{
			DialTLS: func(network string, addr string) (net.Conn, error) {
				conn, err := tls.Dial(network, addr, nil)
				if err != nil {
					return nil, err
				}
				remoteIP, _, _ = net.SplitHostPort(conn.RemoteAddr().String())
				return conn, err
			},
		},
	}
	req, err := http.NewRequest("GET", cfg.watchURL, nil)
	if err != nil {
		// bad URL, log it
		log.Println(err)
		return false, 0, remoteIP
	}
	resp, err := client.Do(req)
	if err != nil {
		// timeout or other connection error, keep silent
		return false, 0, remoteIP
	}
	defer resp.Body.Close()
	// node is alive
	return resp.StatusCode == http.StatusOK, time.Since(t0), remoteIP
}

func (cfg *config) checkNode(ip string) (bool, time.Duration) {
	t0 := time.Now()
	client := &http.Client{
		Timeout: cfg.timeout,
		Transport: &http.Transport{
			DialTLS: func(network string, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				// use the DNS name for the handshake
				c := &tls.Config{
					ServerName: host,
				}
				// connect via IP, not the DNS name
				return tls.Dial(network, ip+":"+port, c)
			},
		},
	}
	req, err := http.NewRequest("GET", cfg.watchURL, nil)
	if err != nil {
		// bad URL, log it
		log.Println(err)
		return false, 0
	}
	resp, err := client.Do(req)
	if err != nil {
		// timeout or other connection error, keep silent
		return false, 0
	}
	defer resp.Body.Close()
	// node is alive
	return resp.StatusCode == http.StatusOK, time.Since(t0)
}

func (cfg *config) watch() {
	mainOk, minTimeout, mainIP := cfg.checkURL()
	if mainIP == "" {
		mainIP, _ = lookupDomain(cfg.domain)
	}

	logMessage := "(" + mainIP + ") "
	if !mainOk {
		logMessage += "FAIL"
	} else {
		logMessage += strconv.Itoa(int(minTimeout/time.Millisecond)) + "ms"
	}

	minIP := ""
	minNode := ""
	for _, n := range cfg.nodes {
		if n.ip == mainIP {
			logMessage = "Active " + n.name + " " + logMessage
			continue
		}
		// Check all nodes
		ok, timeout := cfg.checkNode(n.ip)
		if ok && (minIP == "" || timeout < minTimeout) {
			// the node is stil alive
			minIP = n.ip
			minNode = n.name
			minTimeout = timeout
		}

		// Log stats
		logMessage += ", " + n.name + " "
		if ok {
			logMessage += strconv.Itoa(int(timeout/time.Millisecond)) + "ms"
		} else {
			logMessage += "timeout"
		}
	}
	log.Println(logMessage)

	if !mainOk && minIP != "" {
		// failure of the master node, a working standby node is found
		log.Println("Switch " + minNode + " (" + minIP + ")")
		if err := cfg.cf.moveRecords(mainIP, minIP); err != nil {
			log.Println(err)
		}
	}
}

func main() {
	cfg, err := loadConfig("aw.ini")
	if err != nil {
		log.Println(err)
		return
	}

	// examination
	cfg.watch()
	for range time.NewTicker(cfg.ttl).C {
		cfg.watch()
	}
}
