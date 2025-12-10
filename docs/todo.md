# Kkachi TODO (from requirement-v1.md)

이 파일은 `docs/requirement-v1.md`를 기준으로, 아직 구현되지 않았거나 “향후”로 남겨둔 기능을 정리한 백로그다.  
현재 v1 서버/CLI는 실제 코드 기준으로 동작하며, 아래 항목들은 이후 버전에서 검토할 확장 대상이다.

## CLI

- [ ] `kkachi status --json` 출력 모드
  - requirement-v1에서 제안된 JSON 기반 상태 출력.
  - 현재는 사람이 읽기 좋은 텍스트 포맷만 제공하며, 기계가 파싱하기 위한 별도 `--json` 옵션은 없다.

- [ ] `kkachi status --verbose` 상세 모드
  - requirement-v1 §9의 “CLI 서브커맨드 추가”에서 제안된 상세 모드.
  - 현재 전역 `--verbose` 플래그는 있으나, `kkachi status` 전용의 추가 정보/형식을 제공하는 모드는 없다.

- [ ] `kkachi pending clear` (또는 동등한 pending fix 강제 해제 명령)
  - requirement-v1에서 stale `.kkachi_pending_fix` 처리와 함께 “향후 `kkachi pending clear`” 명령을 예시로 언급.
  - 현재는 `kkachi fix`를 통해서만 pending fix 상태를 해소할 수 있고, 별도의 강제 clear 커맨드는 없다.

## Server / API

- [ ] Web UI (docs 히스토리 및 workspace 상태 조회)
  - requirement-v1 §9 “Web UI” 항목에서 제안.
  - 현재는 REST API(`/docs/*`, `/projects`, `/workspaces/*`, `/state`)만 제공되고, 웹 기반 관리/모니터링 UI는 없다.

- [ ] 더 정교한 자동 merge 전략
  - requirement-v1 §9 “더 정교한 자동 merge 전략”에서 제안.
  - 현재 pre-commit 3-way merge는 단일 전략으로 동작하며, 디렉토리/파일별 우선순위 정책 설정 기능은 없다.

- [ ] 인증/인가 (API 키, 토큰 등)
  - requirement-v1 §8.2에서 “향후 API 키나 토큰 기반 인증”을 제안.
  - 현재는 사내 내부망 전제를 기반으로, 인증/인가 없는 단순 REST 서버로 동작한다.

## 참고 (이미 구현된 항목)

- `kkachi pull`
  - requirement-v1 §9에서 “향후 확장 아이디어”로 제안되었으나,
  - 현재는 `internal/interface/cli/pull.go`, `internal/usecase/docs/pull.go`를 통해 구현되어 있으며 v1에서 사용 가능하다.
