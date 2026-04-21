# The purpose for this project is to create a two-part IRC client

- Backend runs 24/7 and logs messages to sqlite databases, one for each network
- Frontend is a web application acts as a "normal" IRC client, but instead of connecting to the network directly, it connects to the backend and retrieves messages from the database, and sends messages to the backend to be sent to the network
- It's assumed that the front-end connects to the backend securely via Tailscale, so no authentication is needed for the backend. It should run in a Docker container with no ports open to the outside except for the Tailnet.

Future plans:

- TUI client for the backend, partity matching with Web UI as much as possible
- SwiftUI macOS native client
