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
	watchURL string
	timeout  time.Duration
	mainIP   string
	nodes    []node
	cf       *cfAccount
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
	cfg.mainIP = cfg.cf.records["@"].content
	return cfg, nil
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
	fail := false
	minIP := ""
	minTimeout := cfg.timeout
	logMessage := ""

	for _, n := range cfg.nodes {
		// Check all nodes
		ok, timeout := cfg.checkNode(n.ip)
		if !ok && n.ip == cfg.mainIP {
			// main node is broken
			fail = true
		}
		if ok && timeout < minTimeout {
			// the node is stil alive
			minIP = n.ip
			minTimeout = timeout
		}

		// Log stats
		logMessage += n.name + " (" + n.ip + ") "
		okMessage := "standby"
		failMessage := "timeout"
		if n.ip == cfg.mainIP {
			okMessage = "OK"
			failMessage = "FAIL"
		}
		if ok {
			logMessage += strconv.Itoa(int(timeout/time.Millisecond)) + "ms "
			logMessage += okMessage + " "
		} else {
			logMessage += failMessage + " "
		}
	}
	log.Println(logMessage)

	if fail && minIP != "" {
		// failure of the master node, a working standby node is found
		log.Println("Switch to " + minIP)
		if err := cfg.cf.setRecords(minIP); err != nil {
			log.Println(err)
		} else {
			cfg.mainIP = minIP
		}
	}
}

func main() {
	cfg, err := loadConfig("aw.ini")
	if err != nil {
		log.Println(err)
		return
	}

	// show the current master node
	for _, n := range cfg.nodes {
		if n.ip == cfg.mainIP {
			log.Println(n.name + " (" + n.ip + ") selected")
		}
	}

	// examination
	for range time.NewTicker(cfg.ttl).C {
		cfg.watch()
	}
}
