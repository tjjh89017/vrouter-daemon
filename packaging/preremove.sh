#!/bin/bash
systemctl stop vrouter-agent || true
systemctl disable vrouter-agent || true
