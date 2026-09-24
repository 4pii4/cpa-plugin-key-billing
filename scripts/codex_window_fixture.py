#!/usr/bin/env python3

"""TLS proxy fixture for Codex quota-window auto-start E2E verification."""

import argparse
import json
import ssl
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse


ACCOUNTS = {
    "dummy-token-sliding": "sliding",
    "dummy-token-gift": "gift",
    "dummy-token-stable": "stable",
    "dummy-token-disabled": "disabled",
}


class FixtureState:
    def __init__(self):
        now = int(time.time())
        self.lock = threading.Lock()
        self.gifted = False
        self.quota_calls = {name: 0 for name in ACCOUNTS.values()}
        self.packets = []
        self.stable_session_reset = now + 5 * 60 * 60
        self.stable_weekly_reset = now + 7 * 24 * 60 * 60
        self.gift_weekly_reset = now + 2 * 24 * 60 * 60

    def quota(self, account):
        with self.lock:
            self.quota_calls[account] += 1
            now = int(time.time())
            session_reset = now + 5 * 60 * 60
            weekly_reset = now + 7 * 24 * 60 * 60
            session_used = 0
            weekly_used = 0
            allowed = True
            reached = False
            if account == "stable":
                session_reset = self.stable_session_reset
                weekly_reset = self.stable_weekly_reset
                session_used = 23
                weekly_used = 31
            elif account == "gift" and not self.gifted:
                weekly_reset = self.gift_weekly_reset
                weekly_used = 100
                allowed = False
                reached = True
            return {
                "plan_type": "plus",
                "rate_limit": {
                    "allowed": allowed,
                    "limit_reached": reached,
                    "primary_window": {
                        "used_percent": session_used,
                        "limit_window_seconds": 5 * 60 * 60,
                        "reset_at": session_reset,
                    },
                    "secondary_window": {
                        "used_percent": weekly_used,
                        "limit_window_seconds": 7 * 24 * 60 * 60,
                        "reset_at": weekly_reset,
                    },
                },
            }

    def set_gifted(self):
        with self.lock:
            self.gifted = True

    def add_packet(self, account, account_id, body, completed_at):
        expected = {
            "model": "gpt-5.5",
            "input": [{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "hi"}]}],
            "instructions": "Reply with OK.",
            "reasoning": {"effort": "none", "summary": "auto"},
            "max_output_tokens": 8,
            "store": False,
            "stream": True,
        }
        with self.lock:
            self.packets.append(
                {
                    "account": account,
                    "account_id": account_id,
                    "body_exact": body == expected,
                    "completed_at": completed_at,
                }
            )

    def view(self):
        with self.lock:
            return {
                "gifted": self.gifted,
                "quota_calls": dict(self.quota_calls),
                "packets": list(self.packets),
            }


class FixtureHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server_version = "codex-window-fixture"

    def log_message(self, _format, *_args):
        return

    @property
    def state(self):
        return self.server.fixture_state

    def do_CONNECT(self):
        if self.path.split(":", 1)[0].lower() != "chatgpt.com":
            self.send_error(502, "fixture only accepts chatgpt.com")
            return
        self.send_response(200, "Connection Established")
        self.end_headers()
        self.wfile.flush()
        try:
            connection = self.server.tls_context.wrap_socket(self.connection, server_side=True)
        except ssl.SSLError:
            self.close_connection = True
            return
        self.connection = connection
        self.request = connection
        self.rfile = connection.makefile("rb", self.rbufsize)
        self.wfile = connection.makefile("wb", self.wbufsize)
        self.close_connection = False

    def do_GET(self):
        path = urlparse(self.path).path
        if path in ("/health", "/healthz"):
            self.send_json(200, {"status": "ok"})
            return
        if path == "/fixture/state":
            self.send_json(200, self.state.view())
            return
        if path == "/backend-api/wham/usage":
            account = self.account_from_token()
            if not account:
                self.send_json(401, {"error": {"message": "unknown dummy token"}})
                return
            self.send_json(200, self.state.quota(account), close=True)
            return
        self.send_json(404, {"error": {"message": f"no fixture route for {path}"}}, close=True)

    def do_POST(self):
        parsed = urlparse(self.path)
        if parsed.path == "/fixture/gift":
            if parse_qs(parsed.query).get("account") != ["gift"]:
                self.send_json(400, {"error": "invalid account"})
                return
            self.state.set_gifted()
            self.send_json(200, self.state.view())
            return
        if parsed.path == "/backend-api/codex/responses":
            self.handle_codex_packet()
            return
        if parsed.path == "/v1/chat/completions":
            body = self.read_json()
            model = str((body or {}).get("model") or "gpt-4o")
            self.send_json(
                200,
                {
                    "id": "chatcmpl-" + uuid.uuid4().hex,
                    "object": "chat.completion",
                    "created": int(time.time()),
                    "model": model,
                    "choices": [{"index": 0, "message": {"role": "assistant", "content": "OK"}, "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": 8, "completion_tokens": 1, "total_tokens": 9},
                },
            )
            return
        self.send_json(404, {"error": {"message": f"no fixture route for {parsed.path}"}}, close=True)

    def account_from_token(self):
        value = self.headers.get("Authorization", "")
        token = value.removeprefix("Bearer ").strip()
        return ACCOUNTS.get(token)

    def read_json(self):
        try:
            length = int(self.headers.get("Content-Length", "0"))
            return json.loads(self.rfile.read(length) or b"{}")
        except (ValueError, json.JSONDecodeError):
            return None

    def handle_codex_packet(self):
        account = self.account_from_token()
        body = self.read_json()
        if not account or body is None:
            self.send_json(400, {"error": {"message": "invalid tiny packet"}}, close=True)
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Transfer-Encoding", "chunked")
        self.send_header("Connection", "close")
        self.end_headers()
        self.write_chunk(b'data: {"type":"response.created"}\n\n')
        self.wfile.flush()
        time.sleep(0.2)
        self.write_chunk(b'data: {"type":"response.completed","response":{"status":"completed"}}\n\n')
        self.write_chunk(b"")
        self.wfile.flush()
        completed_at = time.time()
        self.state.add_packet(account, self.headers.get("Chatgpt-Account-Id", ""), body, completed_at)
        self.close_connection = True

    def write_chunk(self, payload):
        if payload:
            self.wfile.write(f"{len(payload):X}\r\n".encode("ascii") + payload + b"\r\n")
        else:
            self.wfile.write(b"0\r\n\r\n")

    def send_json(self, status, payload, close=False):
        raw = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        if close:
            self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(raw)
        self.wfile.flush()
        if close:
            self.close_connection = True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=0)
    parser.add_argument("--cert", required=True)
    parser.add_argument("--key", required=True)
    args = parser.parse_args()

    server = ThreadingHTTPServer(("127.0.0.1", args.port), FixtureHandler)
    server.fixture_state = FixtureState()
    server.tls_context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    server.tls_context.load_cert_chain(args.cert, args.key)
    print(f"codex fixture: http://127.0.0.1:{server.server_port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
