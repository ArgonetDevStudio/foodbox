import io
import json
from pathlib import Path
import struct
import tempfile
import unittest
from unittest import mock

import menu_image as tool


USER_ID = 42
NICKNAME = "테스트도시락"
BACKGROUND = "https://images.example.test/profile_resource/" + "a" * 64 + "/f.jpg"
AVATAR = "https://images.example.test/profile_resource/" + "b" * 64 + "/f.jpg"
OTHER = "https://images.example.test/profile_resource/" + "c" * 64 + "/f.jpg"


def record(background=BACKGROUND, user_id=USER_ID, nickname=NICKNAME):
    return json.dumps({"userId": user_id, "suspended": False, "nickname": nickname,
                       "profileImage": {"originalUrl": AVATAR},
                       "backgroundImage": {"originalUrl": background}}, ensure_ascii=False)


def extract(text):
    return tool.extract_candidates([text.encode()], USER_ID, NICKNAME)


class ProfileExtractionTests(unittest.TestCase):
    def test_complete_target_record_excludes_avatar_and_neighbor(self):
        result = extract(record() + record(OTHER, 99, "다른상점"))
        self.assertEqual(result, [{"url": BACKGROUND, "evidence": "complete-response"}])

    def test_escaped_slashes(self):
        self.assertEqual(extract(record().replace("/", "\\/"))[0]["url"], BACKGROUND)

    def test_reused_json_keeps_intact_background_string(self):
        text = record().replace('"profileImage":', '"reused\x00field":')[:-2] + ', "broken'
        self.assertEqual(extract(text), [{"url": BACKGROUND, "evidence": "partial-response"}])

    def test_partial_record_cannot_borrow_next_profile_background(self):
        start = record().split('"backgroundImage"')[0]
        self.assertEqual(extract(start + record(OTHER, 99, "다른상점")), [])

    def test_mismatched_identity_and_unanchored_proximity_are_rejected(self):
        self.assertEqual(extract(record(user_id=99) + BACKGROUND), [])
        self.assertEqual(extract(record(nickname="다른상점")), [])
        self.assertEqual(extract(NICKNAME + BACKGROUND), [])

    def test_nested_background_and_avatar_are_rejected(self):
        text = record().replace('"backgroundImage":', '"nested": {"backgroundImage":')
        self.assertEqual(extract(text), [])
        self.assertEqual(extract(record().replace('"backgroundImage": {', '"backgroundImage": ')), [])

    def test_quoted_field_text_and_later_identity_cannot_claim_ownership(self):
        prefix = record().split('"profileImage"')[0]
        text = prefix + '"status": ' + json.dumps('"backgroundImage":{"originalUrl":"' + OTHER + '"}')
        self.assertEqual(extract(text), [])
        self.assertEqual(extract(prefix + '"nickname":"다른상점","backgroundImage":{"originalUrl":"' + OTHER + '"}'), [])

    def test_multiple_target_versions_remain_explicit_candidates(self):
        result = extract(record() + record(OTHER))
        self.assertEqual({candidate["url"] for candidate in result}, {BACKGROUND, OTHER})

    def test_later_duplicate_identity_rejects_previously_seen_url(self):
        for key, value in [("userId", "99"), ("nickname", '"다른상점"'), ("suspended", "true")]:
            self.assertEqual(extract(record()[:-1] + ',"' + key + '":' + value + '}'), [])

    def test_background_must_be_an_object_and_brackets_must_match(self):
        self.assertEqual(extract(record().replace('"backgroundImage": {', '"backgroundImage": [')), [])
        self.assertEqual(extract(record()[:-2] + "]}"), [])

    def test_truncated_after_intact_url_is_explicitly_partial(self):
        self.assertEqual(extract(record()[:-2]), [{"url": BACKGROUND, "evidence": "partial-response"}])

    def test_corrupt_tail_preserves_only_advisory_prefix(self):
        partial = record()[:-2] + ',"reused\x00key":false}'
        self.assertEqual(extract(partial), [{"url": BACKGROUND, "evidence": "partial-response"}])
        self.assertEqual(extract(partial + ',"userId":99'), [])
        self.assertEqual(extract(partial + ',"backgroundImage":{"originalUrl":"' + OTHER + '"}'), [])

    def test_original_url_must_be_intact_without_thumb_query(self):
        self.assertEqual(extract(record(BACKGROUND + "?type=thumb")), [])
        self.assertEqual(extract(record(BACKGROUND.replace("a" * 64, "a" * 40 + "\x00broken"))), [])

    def test_scanner_frames_are_bounded(self):
        frame = struct.pack("<I", 3) + b"abc"
        self.assertEqual(tool.decode_contexts(frame), [b"abc"])
        for invalid in [b"a", struct.pack("<I", 32769), struct.pack("<I", 5) + b"abc", frame * 129]:
            with self.assertRaises(ValueError):
                tool.decode_contexts(invalid)


class FakeConnection:
    def __init__(self, data, status=200):
        self.data, self.status = data, status

    def __call__(self, host, timeout):
        self.host, self.timeout = host, timeout
        return self

    def request(self, method, path, headers):
        self.method, self.path = method, path

    def getresponse(self):
        response = io.BytesIO(self.data)
        response.status = self.status
        return response

    def close(self):
        pass


class DownloadTests(unittest.TestCase):
    # Minimal SOF segment for testing dimensions, not a real customer image.
    jpeg = bytes.fromhex("ffd8ffc00008080002000300ffd9")

    def test_download_preserves_bytes_and_refuses_overwrite(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "menu.jpg"
            result = tool.download(BACKGROUND, output, FakeConnection(self.jpeg))
            self.assertEqual(output.read_bytes(), self.jpeg)
            self.assertEqual((result["width"], result["height"], result["bytes"]), (3, 2, len(self.jpeg)))
            with self.assertRaises(FileExistsError):
                tool.download(BACKGROUND, output, FakeConnection(self.jpeg))
            self.assertEqual(output.read_bytes(), self.jpeg)

    def test_size_or_invalid_image_does_not_create_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "menu.jpg"
            with mock.patch.object(tool, "MAX_IMAGE_BYTES", 8):
                with self.assertRaises(ValueError):
                    tool.download(BACKGROUND, output, FakeConnection(self.jpeg))
            with self.assertRaises(ValueError):
                tool.download(BACKGROUND, output, FakeConnection(b"<html>expired</html>"))
            self.assertFalse(output.exists())

    def test_only_original_cdn_urls_are_allowed(self):
        for url in [BACKGROUND.replace("https:", "http:"), BACKGROUND + "?credential=secret",
                    BACKGROUND.replace("images.example.test", "localhost"),
                    BACKGROUND.replace("images.example.test", "127.0.0.1"),
                    BACKGROUND.replace("images.example.test", "user:secret@images.example.test"),
                    BACKGROUND.replace("images.example.test", "images.example.test:443"),
                    "http://127.0.0.1/menu.jpg", BACKGROUND + "#fragment"]:
            with self.assertRaises(ValueError):
                tool.validate_url(url)

    def test_redirects_are_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "menu.jpg"
            with self.assertRaises(ValueError):
                tool.download(BACKGROUND, output, FakeConnection(self.jpeg, status=302))
            self.assertFalse(output.exists())

    def test_manifest_selects_only_recorded_candidates_and_is_private(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "private" / "candidates.json"
            result = {"user_id": USER_ID, "nickname": NICKNAME,
                      "candidates": [{"url": BACKGROUND, "evidence": "partial-response"}]}
            tool.save_manifest(result, path)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(tool.selected_candidate(path, 1), BACKGROUND)
            for number in [0, -1, 2]:
                with self.assertRaises(ValueError):
                    tool.selected_candidate(path, number)
            path.write_text(json.dumps({"version": 1, **result, "candidates": [{"url": OTHER}]}))
            with self.assertRaises(ValueError):
                tool.selected_candidate(path, 1)

    def test_private_dns_answers_are_refused_before_connect(self):
        connection = tool.PublicHTTPSConnection("images.example.test", timeout=20)
        for address in ["127.0.0.1", "10.0.0.1", "169.254.169.254", "::1"]:
            with mock.patch.object(tool.socket, "getaddrinfo", return_value=[(0, 0, 0, "", (address, 443))]):
                with mock.patch.object(tool.socket, "create_connection") as create:
                    with self.assertRaises(ValueError):
                        connection.connect()
                    create.assert_not_called()

    def test_checked_public_ip_is_pinned_with_original_tls_hostname(self):
        connection = tool.PublicHTTPSConnection("images.example.test", timeout=20)
        raw = mock.Mock()
        with mock.patch.object(tool.socket, "getaddrinfo", return_value=[(0, 0, 0, "", ("1.1.1.1", 443))]):
            with mock.patch.object(tool.socket, "create_connection", return_value=raw) as create:
                with mock.patch.object(connection._context, "wrap_socket") as wrap:
                    connection.connect()
                    create.assert_called_once_with(("1.1.1.1", 443), 20)
                    wrap.assert_called_once_with(raw, server_hostname="images.example.test")

    def test_png_dimensions_must_be_positive(self):
        png = b"\x89PNG\r\n\x1a\n" + struct.pack(">I", 13) + b"IHDR" + struct.pack(">II", 0, 1)
        with self.assertRaises(ValueError):
            tool.image_dimensions(png)


if __name__ == "__main__":
    unittest.main()
