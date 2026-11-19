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

---

# CDN Geo-Routing Proof Test (Flag Replacement Demo)

This test proves to your professor that the CDN is truly fetching the file from the geographically nearest edge storage server:

### Step 1: Upload an Image on the Website
1. Open the Website (`http://WEBSITE_SERVER_IP`).
2. Upload any image (for example `dog.png`).
3. The image is uploaded and automatically replicated to all storage servers.
4. Copy the **File ID** shown in the table (for example: `f_1794907067_23aa6bc581e6d23b`).

---

### Step 2: SSH into Each Storage Server and Replace the Image
> Note: Files on storage servers are stored in `/var/cdn/data/` named by their **File ID**.

```bash
// ON SERVER #3 (SINGAPORE STORAGE):
// Replace the file content with the Singapore flag:
curl -s -o /var/cdn/data/<FILE_ID> https://flagcdn.com/w640/sg.png


// ON SERVER #4 (US STORAGE):
// Replace the file content with the US flag:
curl -s -o /var/cdn/data/<FILE_ID> https://flagcdn.com/w640/us.png
// (Or if Tokyo Server, use Japan flag: https://flagcdn.com/w640/jp.png)


// ON SERVER #5 (LONDON STORAGE):
// Replace the file content with the UK flag:
curl -s -o /var/cdn/data/<FILE_ID> https://flagcdn.com/w640/gb.png


// (OPTIONAL) LOCK THE FILE SO NOTHING CAN OVERWRITE IT:
sudo chmod 444 /var/cdn/data/<FILE_ID>
// OR MAKE IT COMPLETELY IMMUTABLE:
sudo chattr +i /var/cdn/data/<FILE_ID>

// (TO UNLOCK LATER IF NEEDED):
sudo chattr -i /var/cdn/data/<FILE_ID>
```
*(Replace `<FILE_ID>` with your real File ID, e.g. `/var/cdn/data/f_1794907067_23aa6bc581e6d23b`)*

---

### Step 3: Test with VPN on the Website
Open the Website in your browser and click **View** or **Download** on `dog.png`:

1. **Normal / No VPN (Cambodia / Asia):**
   * Nearest Server: **Singapore**
   * Click **View** on `dog.png` &rarr; You will see the **Singapore Flag**!

2. **Connect VPN to United States:**
   * Nearest Server: **US**
   * Click **View** on `dog.png` &rarr; You will see the **US Flag**!

3. **Connect VPN to United Kingdom (London):**
   * Nearest Server: **London**
   * Click **View** on `dog.png` &rarr; You will see the **UK Flag**!

This proves that even though the file name is always `dog.png`, each client is downloading directly from their geographically closest edge storage node!

