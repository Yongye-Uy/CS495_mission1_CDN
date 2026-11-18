# Server #1: Website Frontend Server Setup

This guide provides step-by-step instructions to configure **Server #1 (Website Dashboard)** on Ubuntu using **Apache** as a reverse proxy.

---

## 1. Update Server & Install Tools
```bash
// UPDATE SERVER
sudo apt update && sudo apt upgrade -y

// INSTALL GIT, CURL, BUILD TOOLS & APACHE
sudo apt install -y git curl wget build-essential apache2

// INSTALL GO (GOLANG)
sudo snap install go --classic

// CHECK GO VERSION
go version
```

---

## 2. Clone Repository & Setup `.env`
```bash
// CREATE APP DIRECTORY
sudo mkdir -p /var/www/cdn
sudo chown -R $USER:$USER /var/www/cdn

// CLONE THE REPO
git clone https://github.com/Yongye-Uy/CS495_mission1_CDN.git /var/www/cdn
cd /var/www/cdn/Website

// CONFIGURE .ENV
cp .env.example .env
nano .env
```
Edit `.env` and set `API_BASE_URL` to **Server #2 (Central API IP)**:
```env
WEBSITE_PORT=3000
API_BASE_URL=http://<CENTRAL_API_SERVER_IP>
```
*(Replace `<CENTRAL_API_SERVER_IP>` with the real IP address of Server #2)*

---

## 3. Build the Website Binary
```bash
cd /var/www/cdn/Website

// BUILD WEBSITE GIN BINARY
go build -o website-server cmd/main.go

// MAKE EXECUTABLE
chmod +x website-server
```

---

## 4. Setup Systemd Service (`cdn-website.service`)
```bash
// CREATE SERVICE FILE
sudo nano /etc/systemd/system/cdn-website.service
```
Paste this configuration:
```ini
[Unit]
Description=CDN Website Dashboard
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/var/www/cdn/Website
ExecStart=/var/www/cdn/Website/website-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```
Start and enable the service:
```bash
// RELOAD SYSTEMD & START SERVICE
sudo systemctl daemon-reload
sudo systemctl start cdn-website
sudo systemctl enable cdn-website

// CHECK SERVICE STATUS
sudo systemctl status cdn-website
```

---

## 5. Configure Apache Reverse Proxy (Port 80 -> Internal 3000)
```bash
// ENABLE REQUIRED APACHE MODULES
sudo a2enmod proxy proxy_http headers rewrite

// CREATE APACHE CONFIGURATION
sudo nano /etc/apache2/sites-available/cdn-website.conf
```
Paste this configuration (replace `WEBSITE_SERVER_IP_OR_DOMAIN` with your Server #1 IP or domain):
```apache
<VirtualHost *:80>
    ServerName WEBSITE_SERVER_IP_OR_DOMAIN

    ProxyPreserveHost On
    ProxyRequests Off

    # Allow up to 100MB file uploads
    LimitRequestBody 104857600

    # Forward all traffic to Go Website on port 3000
    ProxyPass / http://127.0.0.1:3000/
    ProxyPassReverse / http://127.0.0.1:3000/

    ErrorLog ${APACHE_LOG_DIR}/cdn_website_error.log
    CustomLog ${APACHE_LOG_DIR}/cdn_website_access.log combined
</VirtualHost>
```
Enable the site and restart Apache:
```bash
sudo a2ensite cdn-website.conf
sudo a2dissite 000-default.conf
sudo apache2ctl configtest
sudo systemctl restart apache2
```

---

## 6. Access the Website
Open your browser and navigate to:
```text
http://<YOUR_WEBSITE_SERVER_IP>
```
You can now:
* Upload files with drag-and-drop.
* View automatic replication across Singapore, Tokyo, and London.
* View Geolocation routing and distances.
* Add or remove storage workers directly from the web interface.
