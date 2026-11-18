# Server #2: Central API & MongoDB Server Setup

This guide provides step-by-step instructions to configure **Server #2 (Central API + MongoDB)** on Ubuntu using **Apache** as a reverse proxy.

---

## 1. Update Server & Install Tools
```bash
// UPDATE SERVER
sudo apt update && sudo apt upgrade -y

// INSTALL GIT, CURL, BUILD TOOLS & APACHE
sudo apt install -y git curl wget build-essential apache2

// INSTALL GO (GOLANG)
sudo snap install go --classic

// CHECK GO VERSION (Should be 1.22+)
go version
```

---

## 2. Install & Start MongoDB 7.0
```bash
// IMPORT MONGODB GPG KEY
curl -fsSL https://pgp.mongodb.com/server-7.0.asc | \
sudo gpg -o /usr/share/keyrings/mongodb-server-7.0.gpg \
--dearmor

// ADD MONGODB REPOSITORY
echo "deb [ signed-by=/usr/share/keyrings/mongodb-server-7.0.gpg ] https://repo.mongodb.org/apt/ubuntu jammy/mongodb-org/7.0 multiverse" | \
sudo tee /etc/apt/sources.list.d/mongodb-org-7.0.list

// INSTALL MONGODB
sudo apt update
sudo apt install -y mongodb-org

// START AND ENABLE MONGODB SERVICE
sudo systemctl start mongod
sudo systemctl enable mongod
sudo systemctl status mongod
```

---

## 3. Clone Repository & Setup `.env`
```bash
// CREATE APP DIRECTORY
sudo mkdir -p /var/www/cdn
sudo chown -R $USER:$USER /var/www/cdn

// CLONE THE REPO
git clone https://github.com/Yongye-Uy/CS495_mission1_CDN.git /var/www/cdn
cd "/var/www/cdn/API + MongoDB"

// CREATE SHORTCUT SYMLINK (Avoid space issues in path)
sudo ln -s "/var/www/cdn/API + MongoDB" /var/www/cdn/api

// CONFIGURE .ENV
cp .env.example .env
nano .env
```
Ensure `.env` contains:
```env
API_PORT=8080
API_ENV=production
MONGO_URI=mongodb://localhost:27017
MONGO_DB=mini_cdn
MAX_UPLOAD_SIZE_MB=100
```

---

## 4. Build the Central API & CLI Binary
```bash
cd /var/www/cdn/api

// BUILD CENTRAL API DAEMON
go build -o api-server cmd/main.go

// BUILD ONBOARD CLI TOOL
go build -o on_board cmd/on_board/main.go

// MAKE EXECUTABLE
chmod +x api-server on_board
```

---

## 5. Setup Systemd Service (`cdn-api.service`)
```bash
// CREATE SERVICE FILE
sudo nano /etc/systemd/system/cdn-api.service
```
Paste this configuration:
```ini
[Unit]
Description=CDN Central API Coordinator
After=network.target mongod.service

[Service]
Type=simple
User=root
WorkingDirectory=/var/www/cdn/api
ExecStart=/var/www/cdn/api/api-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```
Start and enable the service:
```bash
// RELOAD SYSTEMD & START SERVICE
sudo systemctl daemon-reload
sudo systemctl start cdn-api
sudo systemctl enable cdn-api

// CHECK SERVICE STATUS
sudo systemctl status cdn-api
```

---

## 6. Configure Apache Reverse Proxy (Port 80 -> Internal 8080)
```bash
// ENABLE REQUIRED APACHE MODULES
sudo a2enmod proxy proxy_http headers rewrite

// CREATE APACHE CONFIGURATION
sudo nano /etc/apache2/sites-available/cdn-api.conf
```
Paste this configuration (replace `SERVER_IP_OR_DOMAIN` with your Server #2 IP):
```apache
<VirtualHost *:80>
    ServerName SERVER_IP_OR_DOMAIN

    ProxyPreserveHost On
    ProxyRequests Off

    # Allow up to 100MB file uploads
    LimitRequestBody 104857600

    ProxyPass / http://127.0.0.1:8080/
    ProxyPassReverse / http://127.0.0.1:8080/

    ErrorLog ${APACHE_LOG_DIR}/cdn_api_error.log
    CustomLog ${APACHE_LOG_DIR}/cdn_api_access.log combined
</VirtualHost>
```
Enable the site and restart Apache:
```bash
sudo a2ensite cdn-api.conf
sudo a2dissite 000-default.conf
sudo apache2ctl configtest
sudo systemctl restart apache2
```

---

## 7. Verify & Manage Storage Nodes
```bash
// TEST LOCAL HEALTH CHECK
curl http://localhost/health
// Returns: {"status":"healthy","service":"central-api","database":"connected"}

// LIST REGISTERED STORAGE WORKERS
cd /var/www/cdn/api
./on_board listnodes

// ONBOARD A STORAGE WORKER NODE (Example Singapore)
./on_board addnode ip=STORAGE_SERVER_IP

// CHECK CLUSTER HEALTH
./on_board health
```
