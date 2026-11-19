# Server #3, #4, #5: Storage Server Setup

```bash
// UPDATE SERVER
sudo apt update && sudo apt upgrade -y

// INSTALL APACHE, GIT, RSYNC & TOOLS
sudo apt install -y apache2 git curl wget rsync build-essential

// INSTALL GO (GOLANG)
sudo snap install go --classic

// CHECK GO VERSION
go version


// CREATE STORAGE FOLDER FOR SAVING FILES
sudo mkdir -p /var/cdn/data
sudo chmod -R 777 /var/cdn/data


// GO TO HTML DIRECTORY
cd /var/www/html

// CLONE YOUR PROJECT
git clone https://github.com/Yongye-Uy/CS495_mission1_CDN.git

// GO TO STORAGE FOLDER
cd /var/www/html/CS495_mission1_CDN/Storage

// COPY ENV FILE
// NOTE: LOCATION WILL BE AUTO-DETECTED AUTOMATICALLY BASED ON SERVER IP
// SO YOU DO NOT NEED TO EDIT ANYTHING IN .ENV!
cp .env.example .env

// BUILD STORAGE SERVER
go build -o storage-server cmd/main.go


// CONFIGURE APACHE REVERSE PROXY
// ENABLE PROXY MODULES
sudo a2enmod proxy proxy_http

// GO TO /etc/apache2/sites-available/000-default.conf
sudo nano /etc/apache2/sites-available/000-default.conf

// ADD THIS CODE INSIDE <VirtualHost *:80> BEFORE </VirtualHost>
    ProxyPreserveHost On
    LimitRequestBody 104857600
    ProxyPass / http://127.0.0.1:8081/
    ProxyPassReverse / http://127.0.0.1:8081/

// RESTART APACHE
sudo systemctl restart apache2


// RUN STORAGE SERVER IN BACKGROUND
cd /var/www/html/CS495_mission1_CDN/Storage
nohup ./storage-server > storage.log 2>&1 &

// CHECK IF STORAGE IS WORKING
curl http://localhost/health
// Returns: {"status":"healthy","service":"storage-node","location":"...","nodeId":"..."}

// TO STOP STORAGE SERVER ANYTIME
pkill -f storage-server
```
