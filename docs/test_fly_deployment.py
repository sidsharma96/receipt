"""Local template checks, not a substitute for Fly validation or runtime tests."""
import pathlib
import tomllib
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


class FlyTemplates(unittest.TestCase):
    def test_isolated_apps_and_mounts(self):
        configs = [tomllib.loads((ROOT / "deploy/fly" / name).read_text())
                   for name in ("api.toml", "postgres.toml")]
        self.assertNotEqual(configs[0]["app"], configs[1]["app"])
        for config in configs:
            self.assertTrue(config["app"].startswith("replace-with-new-receipt-"))
            self.assertNotIn("release_command", config["deploy"])
            self.assertEqual(config["mounts"][0]["destination"], "/data")
            self.assertEqual(config["mounts"][0]["initial_size"], "3gb")
            self.assertNotIn("RECEIPT_DATABASE_URL", config["env"])
            self.assertNotIn("POSTGRES_PASSWORD", config["env"])
        self.assertNotEqual(configs[0]["mounts"][0]["source"],
                            configs[1]["mounts"][0]["source"])

    def test_public_api_private_database(self):
        api = tomllib.loads((ROOT / "deploy/fly/api.toml").read_text())
        db = tomllib.loads((ROOT / "deploy/fly/postgres.toml").read_text())
        service, = api["services"]
        self.assertEqual(service["internal_port"], 7777)
        self.assertEqual(service["ports"], [{"port": 443, "handlers": ["tls", "http"]}])
        self.assertEqual(service["auto_stop_machines"], "off")
        self.assertNotIn("services", db)
        self.assertNotIn("http_service", db)
        self.assertEqual(db["env"]["POSTGRES_INITDB_ARGS"], "--auth-host=scram-sha-256")

    def test_service_startup_contract(self):
        dockerfile = (ROOT / "deploy/fly/Dockerfile").read_text()
        entrypoint = (ROOT / "deploy/fly/entrypoint.sh").read_text()
        for flag in ('"--postgres"', '"--credentials"', '"/data/cache"'):
            self.assertIn(flag, dockerfile)
        self.assertIn("util-linux", dockerfile)
        self.assertIn("mountpoint -q /data", entrypoint)
        self.assertIn("exec gosu receipt:receipt", entrypoint)


if __name__ == "__main__":
    unittest.main()
