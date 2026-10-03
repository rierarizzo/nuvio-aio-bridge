# Nuvio AIO Bridge (`nuvio-aio-bridge`)

## 📌 Project Description

Stateless middleware that acts as a bidirectional bridge between **AIOStreams** (and optionally Stremio) and the **Nuvio API**. Its purpose is to synchronize the watch state, playback progress (continue watching), and library (favorites/watchlist) seamlessly for the user.

## 🏗 Architecture

The system requires no database. It utilizes a "Configuration by URL" pattern where session credentials are encrypted and transported within the addon's installation URL itself.

### Data Flow

1. **Configuration (`/configure`):** The user enters their Nuvio credentials (Email/Password). The bridge fetches a `Refresh Token` from the Nuvio API.

2. **Encryption:** The bridge encrypts the `Refresh Token` and generates a configuration hash (`{config_hash}`).

3. **Installation:** An installation link is provided to the user (e.g., `stremio://your-bridge.com/{config_hash}/manifest.json`) to install in AIOStreams.

4. **Event Interception:** AIOStreams sends events (push) or requests data (pull). The bridge intercepts the request, decrypts the `Refresh Token`, fetches a fresh `Access Token` from Nuvio in milliseconds, and executes the action on `api.nuvio.tv`.

## 🛠 Tech Stack

* **Backend:** Go (Golang) - Highly performant and strictly typed, ideal for building lightweight, concurrent middleware.

* **Frontend (Configuration):** Vanilla HTML/CSS/JS served directly by the Go backend.

* **Deployment:** Docker (Single container architecture for isolated and easy deployment).

* **Security:** AES-256-GCM authenticated encryption using Go's native `crypto/aes` and `crypto/cipher` packages. The `SECRET_KEY` required for encryption/decryption will be securely managed via environment variables.

## 🔀 Internal Endpoints (The Bridge)

### 1. Interface and Authentication

* `GET /configure`: Serves the HTML login form.

* `POST /api/login`: Receives credentials, queries Nuvio, encrypts the Refresh Token, and returns the installation URL.

### 2. Addon Protocol (AIOStreams/Stremio)

* `GET /{config_hash}/manifest.json`: Returns the addon manifest declaring support for the `watch_state` resource.

* `POST /{config_hash}/watch_state/push`: Receives event payloads (start, pause, stop, watchlisted) from AIOStreams. Translates the ID (IMDb/TMDb) and makes a POST request to Nuvio.

* `GET /{config_hash}/watch_state/pull`: (If requested by AIOStreams) Queries the watch progress from Nuvio and maps it to the AIOStreams format.

## 🚀 Development Plan (Phases)

### Phase 1: Research and Setup (Day 1)

* \[ \] Initialize the Git repository (`git init`).

* \[ \] Create the project skeleton in Go (`go mod init`).

* \[ \] Document the exact Nuvio endpoints using tools like Postman (Login, Sync Progress, Upsert Library).

### Phase 2: Authentication and Cryptography (Day 2)

* \[ \] Implement AES-256-GCM encryption/decryption logic using Go's `crypto` standard library.

* \[ \] Create the `/api/login` endpoint to communicate with Nuvio's Auth service.

* \[ \] Create the static `/configure` page with the login form.

### Phase 3: Manifest and Data Translation (Day 3)

* \[ \] Implement the `manifest.json` endpoint.

* \[ \] Create the mapping logic: convert ID structures (e.g., `tt1234567`) to Nuvio's internal format if necessary.

* \[ \] Implement silent Access Token renewal using the decrypted Refresh Token.

### Phase 4: Synchronization Endpoints (Days 4-5)

* \[ \] Implement the event receiving route (Push).

* \[ \] Connect the Push route with Nuvio's REST API (Progress and History).

* \[ \] Implement Watchlist (Favorites) handling.

### Phase 5: Testing and Deployment (Day 6)

* \[ \] Create the `Dockerfile` for the Go application (using a multi-stage build for a smaller image).

* \[ \] Perform local testing by connecting AIOStreams to the bridge on `localhost`.

* \[ \] Handle errors (expired tokens, Nuvio connection failures).
