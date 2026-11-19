# Server #1: Website Server Setup

```bash
// UPDATE SERVER
sudo apt update && sudo apt upgrade -y

// INSTALL APACHE, GIT, CURL & BUILD TOOLS
sudo apt install -y apache2 git curl wget build-essential

// INSTALL GO (GOLANG)
sudo snap install go --classic

// CHECK GO VERSION
go version


// GO TO HTML DIRECTORY
cd /var/www/html

// CLONE YOUR PROJECT
git clone https://github.com/Yongye-Uy/CS495_mission1_CDN.git

// GO TO WEBSITE FOLDER
cd /var/www/html/CS495_mission1_CDN/Website

// COPY ENV FILE
cp .env.example .env

// GO TO .ENV FILE TO CONNECT TO API SERVER
nano .env

// CHANGE API_BASE_URL TO YOUR API SERVER IP (SERVER #2)
// Example:
API_BASE_URL=http://IPV4_API_SERVER


// BUILD WEBSITE SERVER
go build -o website-server cmd/main.go


// CONFIGURE APACHE REVERSE PROXY
// ENABLE PROXY MODULES
sudo a2enmod proxy proxy_http

// GO TO /etc/apache2/sites-available/000-default.conf
sudo nano /etc/apache2/sites-available/000-default.conf

// ADD THIS CODE INSIDE <VirtualHost *:80> BEFORE </VirtualHost>
    ProxyPreserveHost On
    LimitRequestBody 104857600
    ProxyPass / http://127.0.0.1:3000/
    ProxyPassReverse / http://127.0.0.1:3000/

// RESTART APACHE
sudo systemctl restart apache2


// RUN WEBSITE SERVER IN BACKGROUND
cd /var/www/html/CS495_mission1_CDN/Website
nohup ./website-server > website.log 2>&1 &

// CHECK IF WEBSITE IS WORKING
curl http://localhost/health

// NOW OPEN IN BROWSER
http://WEBSITE_SERVER_IP

// TO STOP WEBSITE SERVER ANYTIME
pkill -f website-server
```
