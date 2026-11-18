# Server #3, #4, #5: Storage Worker Setup

This guide provides step-by-step instructions to configure a **Storage Worker Server** (Singapore, Tokyo, or London) on Ubuntu using **Apache** as a reverse proxy.

> Repeat these exact steps on every storage server you deploy.

---

## 1. Update Server & Install Tools
```bash
// UPDATE SERVER
sudo apt update && sudo apt upgrade -y

// INSTALL GIT, CURL, RSYNC & APACHE
sudo apt install -y git curl wget rsync build-essential apache2

// INSTALL GO (GOLANG)
sudo snap install go --classic

// CHECK GO VERSION
go version
```

---

## 2. Create Storage Data Directory
```bash
// CREATE DIRECTORY TO STORE UPLOADED FILES
sudo mkdir -p /var/cdn/data
sudo chmod -R 777 /var/cdn/data
```

---

## 3. Clone Repository & Setup `.env`
```bash
// CREATE APP DIRECTORY
sudo mkdir -p /var/www/cdn
sudo chown -R $USER:$USER /var/www/cdn

// CLONE THE REPO
git clone https://github.com/Yongye-Uy/CS495_mission1_CDN.git /var/www/cdn
cd /var/www/cdn/Storage

// CONFIGURE .ENV
cp .env.example .env
nano .env
```
Edit `.env` according to this server's location:
* **For Singapore Server:**
  ```env
  STORAGE_LOCATION=singapore
  STORAGE_NODE_ID=node-singapore
  STORAGE_PORT=8081
  STORAGE_DATA_DIR=/var/cdn/data
  ```
* **For Tokyo Server:**
  ```env
  STORAGE_LOCATION=tokyo
  STORAGE_NODE_ID=node-tokyo
  STORAGE_PORT=8081
  STORAGE_DATA_DIR=/var/cdn/data
  ```
* **For London Server:**
  ```env
  STORAGE_LOCATION=london
  STORAGE_NODE_ID=node-london
  STORAGE_PORT=8081
  STORAGE_DATA_DIR=/var/cdn/data
  ```

---

## 4. Build the Storage Binary
```bash
cd /var/www/cdn/Storage

// BUILD STORAGE SERVER BINARY
go build -o storage-server cmd/main.go

// MAKE EXECUTABLE
chmod +x storage-server
```

---

## 5. Setup Systemd Service (`cdn-storage.service`)
```bash
// CREATE SERVICE FILE
sudo nano /etc/systemd/system/cdn-storage.service
```
Paste this configuration:
```ini
[Unit]
Description=CDN Storage Worker Server
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/var/www/cdn/Storage
ExecStart=/var/www/cdn/Storage/storage-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```
Start and enable the service:
```bash
// RELOAD SYSTEMD & START SERVICE
sudo systemctl daemon-reload
sudo systemctl start cdn-storage
sudo systemctl enable cdn-storage

// CHECK SERVICE STATUS
sudo systemctl status cdn-storage
```

---

## 6. Configure Apache Reverse Proxy (Port 80 -> Internal 8081)
```bash
// ENABLE REQUIRED APACHE MODULES
sudo a2enmod proxy proxy_http headers rewrite

// CREATE APACHE CONFIGURATION
sudo nano /etc/apache2/sites-available/cdn-storage.conf
```
Paste this configuration (replace `STORAGE_SERVER_IP` with your storage server IP):
```apache
<VirtualHost *:80>
    ServerName STORAGE_SERVER_IP

    ProxyPreserveHost On
    ProxyRequests Off

    # Allow up to 100MB file uploads
    LimitRequestBody 104857600

    # Forward all traffic to Go storage app on port 8081
    ProxyPass / http://127.0.0.1:8081/
    ProxyPassReverse / http://127.0.0.1:8081/

    ErrorLog ${APACHE_LOG_DIR}/cdn_storage_error.log
    CustomLog ${APACHE_LOG_DIR}/cdn_storage_access.log combined
</VirtualHost>
```
Enable the site and restart Apache:
```bash
sudo a2ensite cdn-storage.conf
sudo a2dissite 000-default.conf
sudo apache2ctl configtest
sudo systemctl restart apache2
```

---

## 7. Verify Health & Onboard from Central API
```bash
// TEST LOCAL HEALTH CHECK
curl http://localhost/health
// Returns: {"status":"healthy","service":"storage-node","location":"singapore",...}
```

Now, log in to **Server #2 (Central API)** and onboard this storage worker:
```bash
cd /var/www/cdn/api
./on_board addnode ip=STORAGE_SERVER_IP
```
Or simply open your **Website Dashboard**, click **+ Add Storage Node**, enter this storage server's IP, and click **Add Storage Node**!
