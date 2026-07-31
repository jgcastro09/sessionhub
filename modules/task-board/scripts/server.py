#!/usr/bin/env python3
"""
Task Board HTTP Web Server & REST API
Pure Python standard library implementation.
"""

import http.server
import json
import os
import socket
import socketserver
import subprocess
import sys
import urllib.parse
import webbrowser
from pathlib import Path

# Import shared functions from task_board.py
SCRIPT_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(SCRIPT_DIR))

import task_board

APP_DIR = SCRIPT_DIR.parent / "app"


class TaskBoardRequestHandler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(APP_DIR), **kwargs)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path.rstrip("/")

        if path == "/api/cards":
            cards = task_board.load_all_cards()
            self._send_json(cards)
            return

        if path.startswith("/api/cards/"):
            card_id = path.replace("/api/cards/", "").strip()
            card, _ = task_board.load_card_by_id(card_id)
            if card:
                self._send_json(card)
            else:
                self._send_json({"error": "Card não encontrado"}, status=404)
            return

        # Fallback to static file server
        return super().do_GET()

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path.rstrip("/")

        if path == "/api/audit":
            audited = []
            for card in task_board.load_all_cards():
                result = task_board.audit_and_save_card(card)
                audited.append({"id": card["id"], "configured": result["configured"], "passed": result["passed"], "status": card["status"]})
            self._send_json(audited)
            return

        if path.startswith("/api/cards/") and path.endswith("/audit"):
            card_id = path[len("/api/cards/"):-len("/audit")].strip("/")
            card, _ = task_board.load_card_by_id(card_id)
            if not card:
                self._send_json({"error": "Card não encontrado"}, status=404)
                return
            result = task_board.audit_and_save_card(card)
            self._send_json({"card": card, "audit": result})
            return

        if path == "/api/cards":
            body = self._read_body_json()
            if not body:
                self._send_json({"error": "JSON inválido"}, status=400)
                return

            title = body.get("title")
            card_type = body.get("type", "Implementação")
            if not title:
                self._send_json({"error": "Título é obrigatório"}, status=400)
                return

            now_str = task_board.get_now_formatted()
            card_id = task_board.get_next_id()
            status = body.get("status", "Pendente")

            card_data = {
                "id": card_id,
                "title": title,
                "summary": body.get("summary") or title,
                "description": body.get("description") or title,
                "ai_prompt": body.get("ai_prompt", ""),
                "expected_features": body.get("expected_features", ""),
                "impacted_areas": body.get("impacted_areas", []),
                "type": card_type,
                "status": status,
                "priority": body.get("priority", "Média"),
                "created_at": now_str,
                "updated_at": now_str,
                "completed_at": now_str if status == "Implementado" else None,
                "completion_summary": body.get("completion_summary") if status == "Implementado" else None,
                "notes_and_issues": body.get("notes_and_issues"),
                "audit_contract": body.get("audit_contract", ""),
                "audit_report": "",
            }

            task_board.save_card(card_data)
            self._send_json(card_data, status=201)
            return

        self._send_json({"error": "Rota não encontrada"}, status=404)

    def do_PUT(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path.rstrip("/")

        if path.startswith("/api/cards/"):
            card_id = path.replace("/api/cards/", "").strip()
            existing_card, _ = task_board.load_card_by_id(card_id)
            if not existing_card:
                self._send_json({"error": "Card não encontrado"}, status=404)
                return

            body = self._read_body_json()
            if not body:
                self._send_json({"error": "JSON inválido"}, status=400)
                return

            now_str = task_board.get_now_formatted()
            old_status = existing_card.get("status")
            new_status = body.get("status", old_status)

            # Preserve ID and created_at
            existing_card.update({
                "title": body.get("title", existing_card.get("title")),
                "summary": body.get("summary", existing_card.get("summary")),
                "description": body.get("description", existing_card.get("description")),
                "ai_prompt": body.get("ai_prompt", existing_card.get("ai_prompt")),
                "expected_features": body.get("expected_features", existing_card.get("expected_features")),
                "audit_contract": body.get("audit_contract", existing_card.get("audit_contract")),
                "audit_report": "",
                "impacted_areas": body.get("impacted_areas", existing_card.get("impacted_areas")),
                "type": body.get("type", existing_card.get("type")),
                "status": new_status,
                "priority": body.get("priority", existing_card.get("priority")),
                "updated_at": now_str,
                "notes_and_issues": body.get("notes_and_issues", existing_card.get("notes_and_issues"))
            })

            # Check status transitions
            if new_status == "Implementado":
                if not existing_card.get("completed_at"):
                    existing_card["completed_at"] = now_str
                if "completion_summary" in body:
                    existing_card["completion_summary"] = body["completion_summary"]
            elif old_status == "Implementado" and new_status == "Ajuste necessário":
                if "completion_summary" in body:
                    existing_card["completion_summary"] = body["completion_summary"]

            task_board.save_card(existing_card)
            self._send_json(existing_card)
            return

        self._send_json({"error": "Rota não encontrada"}, status=404)

    def do_DELETE(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path.rstrip("/")

        if path.startswith("/api/cards/"):
            card_id = path.replace("/api/cards/", "").strip()
            existing_card, card_file = task_board.load_card_by_id(card_id)
            if not existing_card:
                self._send_json({"error": "Card não encontrado"}, status=404)
                return

            if card_file.exists():
                card_file.unlink()

            self._send_json({"message": f"Card {existing_card['id']} excluído com sucesso"})
            return

        self._send_json({"error": "Rota não encontrada"}, status=404)

    def _read_body_json(self):
        try:
            content_length = int(self.headers.get("Content-Length", 0))
            if content_length > 0:
                raw_data = self.rfile.read(content_length).decode("utf-8")
                return json.loads(raw_data)
        except Exception:
            pass
        return None

    def _send_json(self, data, status=200):
        body = json.dumps(data, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()
        self.wfile.write(body)

    def do_OPTIONS(self):
        self.send_response(204)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def log_message(self, format, *args):
        # Silence static GET logs unless error
        if args and str(args[0]).startswith("GET /api") or str(args[0]).startswith("POST") or str(args[0]).startswith("PUT") or str(args[0]).startswith("DELETE"):
            super().log_message(format, *args)


def get_network_ips():
    tailscale_ips = []
    lan_ips = []
    seen = set()

    def add_ip(candidate):
        if not candidate:
            return
        candidate = candidate.strip()
        if candidate.startswith("127.") or candidate.startswith("169.254.") or candidate in seen:
            return
        parts = candidate.split(".")
        if len(parts) != 4:
            return
        seen.add(candidate)

        # Tailscale CGNAT range: 100.64.0.0/10 (100.64.0.0 - 100.127.255.255)
        if parts[0] == "100":
            try:
                second = int(parts[1])
                if 64 <= second <= 127:
                    tailscale_ips.append(candidate)
                    return
            except ValueError:
                pass
        lan_ips.append(candidate)

    try:
        hostname = socket.gethostname()
        for ip in socket.gethostbyname_ex(hostname)[2]:
            add_ip(ip)
    except Exception:
        pass

    try:
        hostname = socket.gethostname()
        for item in socket.getaddrinfo(hostname, None):
            if item[0] == socket.AF_INET and len(item[4]) > 0:
                add_ip(item[4][0])
    except Exception:
        pass

    if not tailscale_ips and not lan_ips:
        try:
            cmd = ["ipconfig"] if sys.platform.startswith("win") else ["ifconfig"]
            output = subprocess.check_output(cmd, text=True, errors="ignore", stderr=subprocess.DEVNULL)
            for line in output.splitlines():
                if "IPv4" in line or "inet " in line or "Endereço IPv4" in line:
                    parts = line.split(":") if ":" in line else line.split()
                    for part in parts:
                        clean = part.replace("inet", "").strip()
                        add_ip(clean)
        except Exception:
            pass

    return {
        "tailscale": tailscale_ips,
        "lan": lan_ips
    }


class ThreadedTaskBoardServer(socketserver.ThreadingMixIn, socketserver.TCPServer):
    daemon_threads = True
    allow_reuse_address = True


def run_server(port=8090, open_browser=True):
    address = ("", port)
    with ThreadedTaskBoardServer(address, TaskBoardRequestHandler) as httpd:
        localhost_url = f"http://localhost:{port}/"
        network_data = get_network_ips()
        tailscale_ips = network_data["tailscale"]
        lan_ips = network_data["lan"]

        print(f"\n==================================================")
        print(f" NodeStage Task Board Server está ATIVO (Foreground)")
        print(f"==================================================")
        print(f" - Local:              {localhost_url}")

        if tailscale_ips:
            for ip in tailscale_ips:
                print(f" - Tailscale (Celular): http://{ip}:{port}/")
        else:
            print(f" - Tailscale:          (Tailscale não ativo nesta máquina)")

        if lan_ips:
            for ip in lan_ips:
                print(f" - Rede Local (LAN):   http://{ip}:{port}/")

        print(f"--------------------------------------------------")
        print(f" ATENÇÃO:")
        print(f" 1. Este terminal DEVE PERMANECER ABERTO.")
        print(f" 2. Para abrir no celular via Tailscale, use a URL de Tailscale acima.")
        print(f" 3. Se esta janela for fechada, o servidor é desligado.")
        print(f" 4. Para encerrar manualmente: Pressione Ctrl+C.")
        print(f"==================================================\n")

        if open_browser:
            webbrowser.open(localhost_url)

        try:
            httpd.serve_forever()
        except KeyboardInterrupt:
            print("\nServidor encerrado pelo usuário.")


if __name__ == "__main__":
    run_server()
