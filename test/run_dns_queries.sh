#!/bin/bash

# 定义分隔符
SEPARATOR="======================================"

# A record
echo "$SEPARATOR"
echo "Executing: dig example.com A"
dig example.com A
echo "$SEPARATOR"

# AAAA record
echo "$SEPARATOR"
echo "Executing: dig example.com AAAA"
dig example.com AAAA
echo "$SEPARATOR"

# CNAME record
echo "$SEPARATOR"
echo "Executing: dig www.google.com CNAME"
dig www.google.com CNAME
echo "$SEPARATOR"

# PTR record
echo "$SEPARATOR"
echo "Executing: dig 8.8.8.8.in-addr.arpa PTR"
dig 8.8.8.8.in-addr.arpa PTR
echo "$SEPARATOR"

# NS record
echo "$SEPARATOR"
echo "Executing: dig example.com NS"
dig example.com NS
echo "$SEPARATOR"

# MX record
echo "$SEPARATOR"
echo "Executing: dig example.com MX"
dig example.com MX
echo "$SEPARATOR"

# TXT record
echo "$SEPARATOR"
echo "Executing: dig example.com TXT"
dig example.com TXT
echo "$SEPARATOR"

# SRV record
echo "$SEPARATOR"
echo "Executing: dig _xmpp-server._tcp.jabber.org SRV"
dig _xmpp-server._tcp.jabber.org SRV
echo "$SEPARATOR"

# SOA record
echo "$SEPARATOR"
echo "Executing: dig example.com SOA"
dig example.com SOA
echo "$SEPARATOR"
