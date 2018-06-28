# EC2 Degraded Instances Restarter
[![Docker Build](https://img.shields.io/docker/build/alantang888/ec2_degraded_instances_restarter.svg)][docker-hub]
[![Docker Pulls](https://img.shields.io/docker/pulls/alantang888/ec2_degraded_instances_restarter.svg?maxAge=86400)][docker-hub]

This program is call AWS API to find is there any instance running on degraded hardware. If find them, will stop and start affected instance.  
This program loop for every 10 minutes.

## Config
You need to config AWS access token to environment variable:
- AWS_ACCESS_KEY_ID
- AWS_SECRET_ACCESS_KEY
- AWS_REGION

#### Also you can specify SQLite DB file location
Set environment variable `DB_LOC`.

[docker-hub]: https://hub.docker.com/r/alantang888/ec2_degraded_instances_restarter/
