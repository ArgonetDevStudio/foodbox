# 운영 JSON과 대조 기준

운영 파일은 `db/db.json` 형식의 **배열**이다. API의 `{status,error,data}` 응답이나 OCR golden
파일의 ISO 날짜 문자열 형식과 다르다. 구현 근거는 `backend/internal/domain/menu.go`,
`backend/internal/store/file_store.go`, `scripts/db_validate.py`다.

전사 입력:

```json
[{"date":"2026-11-02","menus":["콩나물국","궁중떡볶이","무생채무침"]}]
```

생성되는 운영 레코드:

```json
[{"date":[2026,11,2],"menus":["콩나물국","궁중떡볶이","무생채무침"],"valid":true}]
```

- 날짜는 실존하는 날짜이고 유일해야 한다. 생성·병합 파일은 오래된 날짜부터 정렬한다.
- 항목 순서와 원문은 보존한다. 앞뒤 공백·빈 문자열·알 수 없는 항목은 자동 보정하지 않고 수정할 원본을 확인한다.
- 표의 휴무 문구처럼 항목이 1~2개면 `valid:false`다. 메뉴가 3개 이상인 새 레코드만 `valid:true`다.
- `비빔밥day`처럼 실제 제공 음식을 나타내는 셀의 테마명은 원문 그대로 메뉴에 유지한다.
  `(비빔밥제공)`처럼 앞선 메뉴를 반복하는 안내와 표 하단 공통 고지는 별도 대조 메모에 남긴다.
  실메뉴·재료/소스 설명과 구분이 애매하면 원본을 재확인한다. 메뉴를 임의로 생성하지 않는다.
- 연도가 없으면 요청한 연월·출처 맥락과 요일을 대조한다. 연말 표의 다음 1월은 다음 연도다.
- 기본 범위는 `--month`의 달이다. `--include-following-month`는 표에 인쇄된 **다음 달 1~7일**만
  추가로 허용한다. 날짜 범위를 강제로 맞추려고 없는 날짜·주말·휴무 메뉴를 생성하지 않는다.
- 각 표의 행/열 수와 전사 날짜 수를 대조하고, 모든 날짜·항목을 한 번 더 확인한다.

`menu.json`은 신규 날짜 패치다. 전체 운영 파일처럼 기존 DB에 덮어쓰면 이력이 사라질 수 있다.
기존 DB 사본을 제공받으면 `merge --base ... --patch ... --output ...`으로 별도 후보를 생성한다.
동일 날짜는 패치가 우선하며 기존 다른 날짜, 메뉴 순서, 역사적 `valid` 값과 `menus:null`은 보존한다.
이는 기존 저장소의 읽기 호환성을 위한 것으로, 신규 패치에는 null을 허용하지 않는다.

생성·검증은 **로컬 파일만** 다룬다. `validate`는 JSON 형식·날짜·메뉴/유효성을 검증하지만
이미지 판독의 정확성이나 표의 모든 셀을 읽었다는 사실을 증명하지는 않는다. 따라서 최종 메모에는
대조한 원본 해시, 제목·연월, 표 셀 수, 산출 날짜 수, 안내문 분리, 미해결 항목을 기록한다.
실제 운영 반영까지 요청받으면 저장소의 기존 백업·적용 절차를 따르고, 생성 명령만으로는 DB나
`metadata.json`을 바꾸지 않는다.

## 공통 스킬 호출

공통 `name`/`description` YAML과 `SKILL.md` 본문을 한 곳에서 유지한다.
실제 파일은 `.agents/skills/foodbox-menu/`, `.claude/skills/foodbox-menu`는 그 폴더의 심볼릭 링크다.
Codex에서는 `$foodbox-menu`, Claude Code에서는 `/foodbox-menu`로 호출하거나 목적을 자연어로 요청한다.
새 검색 폴더를 세션 중 만들었으면 도구의 스킬 다시 읽기 기능 또는 새 세션을 사용한다.

규약 근거: [Agent Skills](https://agentskills.io/specification),
[Codex 스킬](https://learn.chatgpt.com/docs/build-skills),
[Claude Code 스킬](https://code.claude.com/docs/en/skills).

## 수정 후 검증

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s .agents/skills/foodbox-menu/scripts -p 'test_*.py' -v
python3 .agents/skills/foodbox-menu/scripts/menu_image.py --help
python3 .agents/skills/foodbox-menu/scripts/menu_json.py --help
```

원본 발견·다운로드 라이브 검증은 이미 열려 있는 대상 창에서 실행하고, 월을 직접 대조한다.
운영 앱 변경 없이 로컬 후보 파일·임시 작업 폴더만 사용한다.
