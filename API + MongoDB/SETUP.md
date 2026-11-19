# Server #2: Central API & MongoDB Server Setup

```bash
// UPDATE SERVER
sudo apt update && sudo apt upgrade -y

// INSTALL APACHE, GIT, CURL & BUILD TOOLS
sudo apt install -y apache2 git curl wget build-essential

// INSTALL GO (GOLANG)
sudo snap install go --classic

// CHECK GO VERSION
go version


// INSTALL MONGODB 7.0
curl -fsSL https://pgp.mongodb.com/server-7.0.asc | \
sudo gpg -o /usr/share/keyrings/mongodb-server-7.0.gpg \
--dearmor

echo "deb [ signed-by=/usr/share/keyrings/mongodb-server-7.0.gpg ] https://repo.mongodb.org/apt/ubuntu jammy/mongodb-org/7.0 multiverse" | \
sudo tee /etc/apt/sources.list.d/mongodb-org-7.0.list

sudo apt update
sudo apt install -y mongodb-org

// START AND ENABLE MONGODB
sudo systemctl start mongod
sudo systemctl enable mongod

// CHECK MONGODB STATUS
sudo systemctl status mongod


// GO TO HTML DIRECTORY
cd /var/www/html

// CLONE YOUR PROJECT
git clone https://github.com/Yongye-Uy/CS495_mission1_CDN.git

// GO TO API FOLDER
cd "/var/www/html/CS495_mission1_CDN/API + MongoDB"

// COPY ENV FILE
cp .env.example .env

// BUILD API SERVER AND ONBOARD CLI
go build -o api-server cmd/main.go
go build -o on_board cmd/on_board/main.go


// CONFIGURE APACHE REVERSE PROXY
// ENABLE PROXY MODULES
sudo a2enmod proxy proxy_http

// GO TO /etc/apache2/sites-available/000-default.conf
sudo nano /etc/apache2/sites-available/000-default.conf

// ADD THIS CODE INSIDE <VirtualHost *:80> BEFORE </VirtualHost>
    ProxyPreserveHost On
    ProxyPass / http://127.0.0.1:8080/
    ProxyPassReverse / http://127.0.0.1:8080/

// RESTART APACHE
sudo systemctl restart apache2


// RUN API SERVER IN BACKGROUND
cd "/var/www/html/CS495_mission1_CDN/API + MongoDB"
nohup ./api-server > api.log 2>&1 &

// CHECK IF API IS WORKING
curl http://localhost/health

// (OPTIONAL) TO ONBOARD A STORAGE NODE FROM TERMINAL
./on_board addnode ip=STORAGE_SERVER_IP

// TO SEE ALL REGISTERED STORAGE NODES
./on_board listnodes

// TO STOP API SERVER ANYTIME
pkill -f api-server
```
