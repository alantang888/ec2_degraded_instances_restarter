# EC2 Degraded Instances Restarter
This program is call AWS API to find is there any instance running on degraded hardware. If find them, will stop and start affected instance.  
This program loop for every 10 minutes.

## Config
You need to config AWS access token to environment variable:
- AWS_ACCESS_KEY_ID
- AWS_SECRET_ACCESS_KEY
- AWS_REGION

#### Also you can specify SQLite DB file location
Set environment variable `DB_LOC`.