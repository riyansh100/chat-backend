"""
TradeFlow Locust Load Test
==========================
Run:
    locust -f locustfile.py --host=http://localhost

Open: http://localhost:8089
"""

import json
import random
import threading
import time
import uuid

import requests
import websocket
from locust import HttpUser, between, events, task


# ── Test users — auto-registered on test start ────────────────────────────────
# These are created via /register before the test runs.
# Passwords stored as bcrypt in Postgres — no manual SQL needed.
SEED_USERS = [
    {"username": "loadtest_1", "password": "load1pass"},
    {"username": "loadtest_2", "password": "load2pass"},
    {"username": "loadtest_3", "password": "load3pass"},
    {"username": "loadtest_4", "password": "load4pass"},
    {"username": "loadtest_5", "password": "load5pass"},
    {"username": "loadtest_6", "password": "load6pass"},
    {"username": "loadtest_7", "password": "load7pass"},
    {"username": "loadtest_8", "password": "load8pass"},
]

INSTRUMENT_IDS = list(range(101, 126))  # 101–125

# Derived at test-start from the --host flag; see _configure_hosts() below.
WS_BASE = None
HOST    = None

_users_registered = False
_register_lock    = threading.Lock()


@events.test_start.add_listener
def _configure_hosts(environment, **kwargs):
    """Derive WS_BASE and HOST from the --host CLI flag so no IPs are hardcoded."""
    global WS_BASE, HOST
    HOST = environment.host.rstrip("/")
    # http -> ws, https -> wss
    ws_scheme = "wss" if HOST.startswith("https") else "ws"
    ws_host   = HOST.replace("https://", "").replace("http://", "")
    WS_BASE   = f"{ws_scheme}://{ws_host}/ws"
    print(f"[setup] HOST={HOST}  WS_BASE={WS_BASE}")


@events.test_start.add_listener
def register_seed_users(environment, **kwargs):
    """
    Runs once before any virtual users spawn.
    Registers all SEED_USERS via /register — idempotent (409 = already exists, fine).
    This way Postgres always has bcrypt hashes for them.
    """
    global _users_registered
    with _register_lock:
        if _users_registered:
            return
        host = environment.host.rstrip("/")
        print("\n[setup] Registering seed users...")
        for u in SEED_USERS:
            try:
                r = requests.post(
                    f"{host}/register",
                    json={"username": u["username"], "password": u["password"]},
                    timeout=10,
                )
                if r.status_code in (200, 201):
                    print(f"[setup]   registered: {u['username']}")
                elif r.status_code == 409:
                    print(f"[setup]   exists:     {u['username']} (ok)")
                else:
                    print(f"[setup]   FAILED:     {u['username']} → {r.status_code} {r.text}")
            except Exception as e:
                print(f"[setup]   ERROR:      {u['username']} → {e}")
        _users_registered = True
        print("[setup] Seed users ready.\n")


# ══════════════════════════════════════════════════════════════════════════════
#  HTTP USER — REST endpoints (login, subscribe, history, logout)
# ══════════════════════════════════════════════════════════════════════════════
class TradeFlowHttpUser(HttpUser):
    wait_time = between(0.5, 2)
    token     = None

    def on_start(self):
        time.sleep(random.uniform(0, 2)) 
        user = random.choice(SEED_USERS)
        with self.client.post(
            "/login",
            json={"username": user["username"], "password": user["password"]},
            catch_response=True,
            name="/login",
        ) as resp:
            if resp.status_code == 200:
                self.token = resp.json().get("token")
                resp.success()
            else:
                resp.failure(f"{resp.status_code} {resp.text}")

    def _headers(self):
        return {"Authorization": f"Bearer {self.token}", "Content-Type": "application/json"}

    @task(3)
    def get_subscriptions(self):
        if not self.token:
            return
        self.client.get("/subscriptions", headers=self._headers(), name="/subscriptions")

    @task(2)
    def subscribe_random(self):
        if not self.token:
            return
        self.client.post(
            "/subscribe",
            json={"instrument_id": random.choice(INSTRUMENT_IDS)},
            headers=self._headers(),
            name="/subscribe",
        )

    @task(1)
    def unsubscribe_random(self):
        if not self.token:
            return
        self.client.post(
            "/unsubscribe",
            json={"instrument_id": random.choice(INSTRUMENT_IDS)},
            headers=self._headers(),
            name="/unsubscribe",
        )

    @task(1)
    def get_history(self):
        if not self.token:
            return
        instr     = random.choice(INSTRUMENT_IDS)
        indicator = random.choice(["sma", "ema", "rsi", "macd", "bb", "ohlc"])
        self.client.get(
            f"/history?instrument={instr}&indicator={indicator}&resolution=1m",
            headers=self._headers(),
            name="/history",
        )

    def on_stop(self):
        if self.token:
            self.client.post("/logout", headers=self._headers(), name="/logout")


# ══════════════════════════════════════════════════════════════════════════════
#  WEBSOCKET USER — login + persistent WS + join all 25 rooms
# ══════════════════════════════════════════════════════════════════════════════
class WebSocketUser(HttpUser):
    wait_time = between(0.5, 2)
    token     = None
    ws_conn   = None

    def on_start(self):
        time.sleep(random.uniform(0, 2)) 
        user = random.choice(SEED_USERS)
        with self.client.post(
            "/login",
            json={"username": user["username"], "password": user["password"]},
            catch_response=True,
            name="/login [ws-user]",
        ) as resp:
            if resp.status_code == 200:
                self.token = resp.json().get("token")
                resp.success()
            else:
                resp.failure(f"{resp.status_code} {resp.text}")
                return

        t = threading.Thread(target=self._run_ws, daemon=True)
        t.start()

    def _run_ws(self):
        url = f"{WS_BASE}?token={self.token}"
        self.ws_conn = websocket.WebSocketApp(
            url,
            on_open=self._on_open,
            on_message=self._on_message,
            on_error=self._on_error,
            on_close=self._on_close,
        )
        self.ws_conn.run_forever()

    def _on_open(self, ws):
        for instr_id in INSTRUMENT_IDS:
            ws.send(json.dumps({"type": "join", "room": str(instr_id)}))

    def _on_message(self, ws, message):
        events.request.fire(
            request_type="WS",
            name="message_received",
            response_time=0,
            response_length=len(message),
            exception=None,
            context={},
        )

    def _on_error(self, ws, error):
        # Report errors under a distinct name so they don't inflate
        # message_received failure counts. OSError(64) here means the
        # server closed the socket — visible as "WS connection_error" in
        # the Locust UI so you can track it separately from normal traffic.
        events.request.fire(
            request_type="WS",
            name="connection_error",
            response_time=0,
            response_length=0,
            exception=error,
            context={},
        )

    def _on_close(self, ws, code, msg):
        pass

    @task
    def stay_connected(self):
        pass

    def on_stop(self):
        if self.ws_conn:
            self.ws_conn.close()
        if self.token:
            self.client.post(
                "/logout",
                headers={"Authorization": f"Bearer {self.token}"},
                name="/logout [ws-user]",
            )