# aw

AW is utility for switching DNS-records to ensure the availability of a web server

This repository has been archived. It is now read-only. 

## Usage

The IP address of the primary website server is stored on the DNS service. In this case, it is cloudflare.com.
The secondary or standby servers will only come into play when the primary server is unavailable.

Lets assume, that www.example.com now point to 10.0.0.11 address.
Standby server IPs are 10.0.0.12 and 10.0.0.13.

![AW workflow scheme](https://codeation.github.io/images/aw_scheme.png)

In this case, the cloudflare DNS records will be:

```
Type    Name            Value                   TTL         Status
A       example.com     points to 10.0.0.11     Automatic   DNS only
A       www             points to 10.0.0.11     Automatic   DNS only
A       *               points to 10.0.0.11     Automatic
...
```

The AW utility will monitor all servers.
If the primary server does not respond, DNS records will be switched to the standby server.

## Installation

The AW utility can be built using the following command:

```
go get -u github.com/codeation/aw
```

The -u flag instructs get to use the network to update the named packages and their dependencies.
By default, get uses the network to check out missing packages but does not use it to look for updates
to existing packages.

The AW executable file will be in $GOPATH/bin directory.

## aw.ini

Sample configuration file:

```
; Watch interval, seconds
ttl=120
url=https://www.example.com/index.html
; Timeout to get URL, seconds
timeout=10

; CloudFlare account and records
apikey=012****************a12
domain=example.com
email=admin@example.com
names=@,*,www

; nodes alias and ip
[nyc01]
ip=10.0.0.11

[nyc02]
ip=10.0.0.12

[sea01]
ip=10.0.0.13
```

The aw.ini must be in the working directory.

## Daemon

This is example of /etc/systemd/system/aw.service file, replace file paths and yours user and group name.
No any sudo privelegies are needed.

```
[Unit]
Description=aw
After=network.target
Requires=network.target

[Service]
Type=simple
User=username
Group=groupname
WorkingDirectory=/home/support/aw
ExecStart=/home/support/aw/aw
LimitSTACK=1048576
OOMScoreAdjust=-100
Restart=always
TimeoutSec=60

[Install]
WantedBy=multi-user.target
```

To enable and start AW as service, please run:

```
sudo systemctl enable aw.service
sudo systemctl start aw.service
```

To stop and disable:

```
sudo systemctl stop aw.service
sudo systemctl disable aw.service
```

Logs will be collected into /var/log/syslog file, to view:

```
sudo cat /var/log/syslog | grep aw
```

or

```
cat /var/log/syslog | grep " aw\[" | tail -n 20 | cut -d ' ' -f 7-
```

## IPv6

You can specify an IPv6 address for all or some of your servers, if they are accessible via IPv6.
All servers are monitored using the IPv4 protocol.
Thus, the AW utility can be deployed on the server without a IPv6 protocol.

Sample aw.ini file with IPv6 addresses:

```
; Watch interval, seconds
ttl=120
url=https://www.example.com/index.html
; Timeout to get URL, seconds
timeout=10

; CloudFlare account and records
apikey=012****************a12
domain=example.com
email=admin@example.com
names=@,*,www

; nodes alias and ip
[nyc01]
ip=10.0.0.11
ipv6=2001:db8:85a3::8a2e:370:7334

[nyc02]
ip=10.0.0.12
ipv6=2001:db8:85a3::1e7a:629:2665

[sea01]
ip=10.0.0.13
```

When the AW utility finds that it is necessary to change the IPv4 address of the domain,
the IPv6 domain address will also be changed synchronously.
If the elected server does not support the IPv6 protocol, the AAAA-records will be deleted.
Conversely, if there were no AAAA-records, and the elected server supports the IPv6 protocol,
AAAA-records will be made.
