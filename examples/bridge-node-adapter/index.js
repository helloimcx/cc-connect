const WebSocket = require("ws");
const readline = require("readline");

const wsURL = process.env.CC_CONNECT_WS_URL || "ws://127.0.0.1:9810/bridge/ws";
const token = process.env.CC_CONNECT_BRIDGE_TOKEN || "";
const platform = process.env.CC_CONNECT_PLATFORM || "node-demo";
const project = process.env.CC_CONNECT_PROJECT || "";
const userID = process.env.CC_CONNECT_USER_ID || "demo-user";
const userName = process.env.CC_CONNECT_USER_NAME || "Demo User";
const scope = process.env.CC_CONNECT_SCOPE || userID;
const sessionKey = process.env.CC_CONNECT_SESSION_KEY || `${platform}:${scope}:${userID}`;

let nextID = 1;
let registered = false;

const headers = {};
if (token) {
  headers.Authorization = `Bearer ${token}`;
}

const ws = new WebSocket(wsURL, { headers });

ws.on("open", () => {
  const register = {
    type: "register",
    platform,
    capabilities: ["text"]
  };
  if (project) {
    register.project = project;
  }
  ws.send(JSON.stringify(register));
});

ws.on("message", (raw) => {
  let msg;
  try {
    msg = JSON.parse(raw.toString());
  } catch (err) {
    console.error("invalid JSON from bridge:", err.message);
    return;
  }

  switch (msg.type) {
    case "register_ack":
      if (!msg.ok) {
        console.error("register failed:", msg.error || "unknown error");
        process.exitCode = 1;
        ws.close();
        return;
      }
      registered = true;
      console.log(`registered platform=${platform} project=${msg.project || ""} session_key=${sessionKey}`);
      console.log("type a message and press enter");
      break;
    case "reply":
      console.log(`assistant: ${msg.content}`);
      break;
    case "card":
      console.log("assistant(card):", JSON.stringify(msg.card));
      break;
    case "buttons":
      console.log(`assistant(buttons): ${msg.content}`);
      break;
    case "pong":
      break;
    default:
      console.log("bridge:", JSON.stringify(msg));
  }
});

ws.on("close", () => {
  console.log("bridge connection closed");
  process.exit();
});

ws.on("error", (err) => {
  console.error("bridge connection error:", err.message);
});

setInterval(() => {
  if (ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: "ping", ts: Date.now() }));
  }
}, 30000);

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout,
  terminal: true
});

rl.on("line", (line) => {
  if (!registered || ws.readyState !== WebSocket.OPEN) {
    console.error("adapter is not ready yet");
    return;
  }
  const trimmed = line.trim();
  if (!trimmed) {
    return;
  }
  const msg = {
    type: "message",
    msg_id: `msg-${nextID}`,
    session_key: sessionKey,
    user_id: userID,
    user_name: userName,
    content: trimmed,
    reply_ctx: `ctx-${nextID}`
  };
  nextID += 1;
  ws.send(JSON.stringify(msg));
});
