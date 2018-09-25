# aw

AW is utility for switching DNS-records to ensure the availability of a web server

## Usage

The IP address of the primary website server is stored on the DNS service. In this case, it is cloudflare.com.
The secondary or standby servers will only come into play when the primary server is unavailable.

Lets assume, that www.example.com now point to 10.0.0.11 address.
Standby server IPs are 10.0.0.12 and 10.0.0.13.

![AW workflow scheme](https://codeation.github.io/pages/images/aw_scheme.png)

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
url=https://www.webexcerpt.com/index.html
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
