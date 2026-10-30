import copy
import datetime as dt
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("menu_json", Path(__file__).with_name("menu_json.py"))
menu_json = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(menu_json)


class MenuJSONTests(unittest.TestCase):
    def cells(self):
        return [{"date": "2026-11-03", "menus": ["국", "반찬/소스", "김치"]},
                {"date": "2026-11-02", "menus": ["국", "생채", "국", "김치"]}]

    def test_build_sorts_dates_preserving_menu_order_and_duplicates(self):
        rows = menu_json.build(self.cells(), (2026, 11))
        self.assertEqual(rows[0], {"date": [2026, 11, 2], "menus": ["국", "생채", "국", "김치"], "valid": True})

    def test_holiday_and_two_items_are_invalid_new_menus(self):
        for items in [["휴무"], ["국", "밥"]]:
            rows = menu_json.build([{"date": "2026-11-02", "menus": items}], (2026, 11))
            self.assertFalse(rows[0]["valid"])

    def test_three_items_are_valid(self):
        self.assertTrue(menu_json.build(self.cells(), (2026, 11))[1]["valid"])

    def test_input_must_have_exact_fields(self):
        for bad in [{"date": "2026-11-02", "menus": [], "valid": True}, {"date": "2026-11-02"}, None]:
            with self.assertRaises(ValueError):
                menu_json.build([bad], (2026, 11))

    def test_empty_transcription_is_not_ready(self):
        for empty in [[], {}, None]:
            with self.assertRaises(ValueError):
                menu_json.build(empty, (2026, 11))

    def test_duplicate_dates_fail(self):
        cells = self.cells()
        cells[1]["date"] = cells[0]["date"]
        with self.assertRaises(ValueError):
            menu_json.build(cells, (2026, 11))

    def test_invalid_or_noncanonical_iso_dates_fail(self):
        for date in ["2026-02-29", "2026-13-02", "2026-11-31", "2026-1-02", [2026, 11, 2], True]:
            with self.subTest(date=date), self.assertRaises(ValueError):
                menu_json.build([{"date": date, "menus": ["휴무"]}], (2026, 11))

    def test_leap_day_valid(self):
        self.assertEqual(menu_json.build([{"date": "2028-02-29", "menus": ["휴무"]}], (2028, 2))[0]["date"], [2028, 2, 29])

    def test_next_month_explicit_first_week_only(self):
        cells = [{"date": "2026-12-04", "menus": ["휴무"]}]
        with self.assertRaises(ValueError):
            menu_json.build(cells, (2026, 11))
        self.assertEqual(len(menu_json.build(cells, (2026, 11), True)), 1)
        cells[0]["date"] = "2026-12-08"
        with self.assertRaises(ValueError):
            menu_json.build(cells, (2026, 11), True)

    def test_next_month_crosses_year(self):
        self.assertTrue(menu_json.in_scope(dt.date(2027, 1, 4), (2026, 12), True))
        self.assertFalse(menu_json.in_scope(dt.date(2026, 1, 4), (2026, 12), True))

    def test_out_of_month_does_not_get_silently_filtered(self):
        with self.assertRaises(ValueError):
            menu_json.build(self.cells(), (2026, 10))

    def test_new_menu_rejects_null_blank_untrimmed_or_nonstring(self):
        for items in [None, [], [""], ["  "], [" 국"], ["국 "], ["국", 3]]:
            with self.subTest(items=items), self.assertRaises(ValueError):
                menu_json.build([{"date": "2026-11-02", "menus": items}], (2026, 11))

    def test_operating_shape_integer_dates_and_boolean_valid(self):
        good = {"date": [2026, 11, 2], "menus": ["휴무"], "valid": False}
        for field, value in [("date", [True, 11, 2]), ("date", "2026-11-02"), ("valid", 0), ("valid", True)]:
            bad = dict(good, **{field: value})
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                menu_json.check_rows([bad])

    def test_operating_dates_must_be_unique_and_sorted(self):
        rows = menu_json.build(self.cells(), (2026, 11))
        for bad in [list(reversed(rows)), [rows[0], rows[0]]]:
            with self.assertRaises(ValueError):
                menu_json.check_rows(bad)

    def test_merge_last_write_wins_preserves_historical_values(self):
        base = [{"date": [2026, 10, 1], "menus": None, "valid": True},
                {"date": [2026, 10, 2], "menus": ["", "두번째"], "valid": False},
                {"date": [2026, 11, 2], "menus": ["이전"], "valid": False}]
        before = copy.deepcopy(base)
        patch = menu_json.build(self.cells(), (2026, 11))
        result = menu_json.merge(base, patch)
        self.assertEqual(result[:2], base[:2])
        self.assertEqual(result[2], patch[0])
        self.assertEqual(base, before)
        self.assertEqual(len(result), 4)

    def test_legacy_accepts_empty_database_but_not_wrong_types(self):
        self.assertEqual(menu_json.check_rows([], legacy=True), [])
        for row in [{"date": [2026, 1, 1], "menus": [3], "valid": False},
                    {"date": [2026, 1, 1], "menus": None, "valid": "false"}]:
            with self.assertRaises(ValueError):
                menu_json.check_rows([row], legacy=True)

    def test_write_never_overwrites_sources_or_existing_output(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "menu.json"
            path.write_text("original")
            with self.assertRaises(ValueError):
                menu_json.write_new(path, [], [path])
            with self.assertRaises(FileExistsError):
                menu_json.write_new(path, [])
            self.assertEqual(path.read_text(), "original")
            destination = Path(directory) / "new.json"
            menu_json.write_new(destination, menu_json.build(self.cells(), (2026, 11)))
            self.assertEqual(len(json.loads(destination.read_text())), 2)


if __name__ == "__main__":
    unittest.main()
