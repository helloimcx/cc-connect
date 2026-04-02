# Node.js Bridge Adapter Example

This is a minimal adapter example for the cc-connect bridge WebSocket server.

## Install

```bash
cd examples/bridge-node-adapter
npm install
```

## Run

```bash
CC_CONNECT_WS_URL=ws://127.0.0.1:9810/bridge/ws \
CC_CONNECT_BRIDGE_TOKEN=your-bridge-token \
CC_CONNECT_PROJECT=demo-project \
npm start
```

Optional environment variables:

- `CC_CONNECT_PLATFORM` default: `node-demo`
- `CC_CONNECT_SESSION_KEY` default: `<platform>:<scope>:<user_id>`
- `CC_CONNECT_SCOPE` default: `user_id`
- `CC_CONNECT_USER_ID` default: `demo-user`
- `CC_CONNECT_USER_NAME` default: `Demo User`

After registration succeeds, type a line in stdin and the adapter will send it to cc-connect as a bridge `message`.
