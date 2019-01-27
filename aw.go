package main

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codeation/inifile"
)

type node struct {
	name string
	ip   string
	ipv6 string
}

type config struct {
	ttl      time.Duration
	domain   string
	watchURL string
	timeout  time.Duration
	nodes    []node
	cf       *cfAccount
}

func isAddrEqual(left, right string) bool {
	leftIP := net.ParseIP(left)
	rightIP := net.ParseIP(right)
	if rightIP == nil {
		return leftIP == nil
	}
	return rightIP.Equal(leftIP)
}

func lookupProtocolDomain(protocol string, domain string) (string, error) {
	ips, err := net.LookupIP(domain)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if strings.ToLower(protocol) == "ipv6" && len(ip) == 16 {
			return ip.String(), nil
		} else if strings.ToLower(protocol) == "ipv4" && len(ip) == 4 {
			return ip.String(), nil
		} else if strings.ToLower(protocol) == "ipv4" && len(ip) == 16 && ip.To4() != nil {
			return ip.String(), nil
		}
	}
	// lookup without errors
	return "", nil
}

func lookupDomain(domain string) (string, error) {
	return lookupProtocolDomain("IPv4", domain)
}

func lookupDomainIPv6(domain string) (string, error) {
	return lookupProtocolDomain("IPv6", domain)
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
			ipv6: ini.Get(name, "ipv6"),
		})
	}

	cfg.cf, err = newAccount(ini)
	if err != nil {
		return nil, err
	}
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
	// actual DNS records
	actualIP, err := lookupDomain(cfg.domain)
	if err != nil {
		log.Println("DNS lookup failure")
		return
	}
	actualIPv6, _ := lookupDomainIPv6(cfg.domain) // ignore errors
	// active node IPs
	selectedIPv6 := ""
	selectedNode := ""
	// fastest node IPs
	minIP := ""
	minIPv6 := ""
	minNode := ""
	minTimeout := cfg.timeout
	logMessage := ""
	for _, n := range cfg.nodes {
		if logMessage != "" {
			logMessage += ", "
		}
		// Check node
		ok, timeout := cfg.checkNode(n.ip)
		logMessage += n.name
		// if node is actual, note that
		if isAddrEqual(n.ip, actualIP) {
			logMessage += " (" + n.ip
			if actualIPv6 != "" && isAddrEqual(actualIPv6, n.ipv6) {
				logMessage += ", " + n.ipv6
			}
			logMessage += ")"
			if ok {
				selectedIPv6 = n.ipv6
				selectedNode = n.name
			}
		}
		// lookup fastest node
		if ok && timeout < minTimeout {
			minIP = n.ip
			minIPv6 = n.ipv6
			minNode = n.name
			minTimeout = timeout
		}
		// log node status
		if ok {
			logMessage += " " + strconv.Itoa(int(timeout/time.Millisecond)) + "ms"
		} else {
			logMessage += " Fail"
		}
	}
	log.Println(logMessage)
	if selectedNode == "" && minIP != "" {
		// acting node fail, select fastest node
		log.Println("Switch IPv4 to " + minNode + " (" + minIP + ")")
		if err := cfg.cf.moveRecords(actualIP, minIP); err != nil {
			log.Println(err)
		}
	}
	if selectedNode != "" && selectedIPv6 != actualIPv6 {
		// correct IPv6 for acting node
		log.Println("Switch IPv6 to " + selectedNode + " (" + selectedIPv6 + ")")
		if err := cfg.cf.moveRecordsIPv6(actualIPv6, selectedIPv6); err != nil {
			log.Println(err)
		}
	}
	if selectedNode == "" && minIPv6 != actualIPv6 {
		// acting node fail, select fastest node IPv6
		log.Println("Switch IPv6 to " + minNode + " (" + minIPv6 + ")")
		if err := cfg.cf.moveRecordsIPv6(actualIPv6, minIPv6); err != nil {
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
