# Client Roadmap (v3) — Web Terminal UI

> 이 문서는 v3 클라이언트(kkachi-web) 구현을 **5개 PR(CTASK-1~5)** 로 쪼개기 위한 로드맵이다.  
> 각 CTASK는 Git PR tag로 사용된다.

---

## 0. PR 컨벤션

- PR 제목 예시: `[CTASK-3] xterm + WebSocket attach (streaming + resize)`
- 원칙
  - 각 PR은 “논리적으로 완결”되어야 한다(단독으로 리뷰/테스트 가능).
  - 서버 STASK 단계에 맞춰 계약(요청/응답/WS)을 점진적으로 확정한다.
  - v3 비범위인 “세션 복구/재연결”을 암묵적으로 도입하지 않는다.

---

## 1. v3 클라이언트 산출물 요약(최종 상태)

### 1.1 `/terminal` 페이지 UX
- 좌측: 열린 console(=PTY 세션) 리스트
- 우측: 선택된 console의 터미널(xterm)
- `New Console`로 workspace 선택 → 해당 cwd에서 쉘 오픈
- console 복수 생성/전환
- 드래그&드롭으로 console 순서 변경
- console 닫기(세션 종료)

### 1.2 데이터/계약
- workspace 목록: `GET /api/state`의 `workspaces[]`
- session create/terminate: `POST/DELETE /api/pty/sessions...`
- streaming: `WS /api/pty/sessions/{id}/ws`

---

## 2. CTASK 개요

| CTASK | 목표(한 줄) | 선행(권장) | 핵심 테스트 |
|---|---|---|---|
| CTASK-1 | /terminal 스캐폴딩 + 상태 모델 + api 모듈 골격 | - | 라우트/레이아웃 렌더 |
| CTASK-2 | Workspace picker + 세션 생성(HTTP) + 콘솔 리스트(WS 없음) | STASK-1 | picker→create 호출 |
| CTASK-3 | xterm + WS attach + 스트리밍(단일 콘솔 E2E) | STASK-2 | echo roundtrip (mock 또는 e2e) |
| CTASK-4 | multi-console + 전환(연결 유지) + 정리 | STASK-2 | 2콘솔 생성/전환 |
| CTASK-5 | DnD reorder + (선택)정렬 저장 + auth 토큰 대응 + UX/테스트 마감 | STASK-5(토큰) | reorder 회귀 + auth on/off |

---

## 3. CTASK 상세

## CTASK-1 — /terminal 스캐폴딩 + 상태 모델 + API 모듈 골격

### 목적
- v2에서 확정한 프론트 구조를 유지하며 `/terminal` 페이지의 기본 골격을 만든다.
- 이후 작업이 쌓이기 쉬운 **상태 모델/컴포넌트 경계/API 모듈**을 먼저 확정해, PR 간 충돌을 줄인다.

### 포함 범위
- 라우트 추가: `/terminal`
- 페이지 레이아웃(좌측 리스트 / 우측 패널 / 상단 툴바)
- console 상태 모델(타입, store/reducer/hook)
- API 모듈 스텁
  - `api/state.ts`
  - `api/pty.ts`
- 빈 상태/로딩/에러 공통 컴포넌트 재사용 구조

### 구현 가이드
1) **파일/폴더(권장)**
- `web/src/pages/TerminalPage.tsx`
- `web/src/components/terminal/ConsoleList.tsx`
- `web/src/components/terminal/TerminalPane.tsx`
- `web/src/components/terminal/WorkspacePickerModal.tsx` (placeholder)
- `web/src/api/state.ts`
- `web/src/api/pty.ts`

2) **상태 모델(예시)**
```ts
export type ConsoleStatus = 'CREATED' | 'CONNECTING' | 'CONNECTED' | 'CLOSED' | 'ERROR';

export type ConsoleRecord = {
  consoleId: string;      // client uuid
  sessionId?: string;     // server session id
  workspaceId?: string;
  project?: string;
  title: string;
  status: ConsoleStatus;
  createdAt: number;
  errorMessage?: string;

  // runtime only
  xterm?: any;
  ws?: WebSocket;
};
```

3) **원칙**
- 컴포넌트에서 fetch 직접 호출 금지: `api/*`에 모아둔다.
- 세션 복구/재연결 비범위: 새로고침 시 열린 콘솔을 자동 복구하지 않는다.

### 테스트/검증
- 라우트 렌더 테스트
- console reducer/hook 기본 동작(add/select/remove) 단위 테스트

### 완료 기준
- `/terminal` 진입이 가능하고 UI 골격이 보인다.
- 다음 CTASK에서 workspace picker/API를 얹기 쉬운 구조가 잡혔다.

---

## CTASK-2 — Workspace Picker + 세션 생성(HTTP) + 콘솔 리스트(WS 없음)

### 목적
- workspace를 선택해 **세션 생성(HTTP)** 까지 완료하고, 좌측 리스트에서 콘솔이 생성/선택되는 UX를 완성한다.
- WS/xterm은 CTASK-3에서 다룬다.

### 선행
- 서버 STASK-1(HTTP create/terminate)

### 포함 범위
- workspace 목록 로딩: `GET /api/state`
- Workspace picker(검색/선택)
- `POST /api/pty/sessions` 호출
- 콘솔 리스트 생성/선택/닫기(닫기는 `DELETE` 호출까지 포함)

### 구현 가이드
1) **Workspace Picker UI**
- 표시: project, workspace_id, local_path(말줄임 + tooltip 권장)
- 검색: workspace_id/local_path 부분 일치

2) **세션 생성 플로우**
- 선택 → `createSession()` 호출
- 응답의 `session_id`/`ws_url`을 console record에 저장
- status는 `CREATED` 또는 `CONNECTING`으로 둔다(WS는 아직 안 붙음)

3) **닫기(terminate) 플로우**
- console 닫기 클릭 → `DELETE /api/pty/sessions/{id}` 호출
- 성공/실패와 무관하게 UI에서는 콘솔을 제거하거나 `CLOSED`로 전환(정책 고정)

### 테스트/검증
- Component 테스트: picker → 선택 → API 호출 → 리스트에 항목 생성
- Error 테스트: `unknown_workspace` 등 에러 코드별 사용자 메시지 매핑(최소)

### 완료 기준
- 사용자가 workspace를 선택하면 콘솔 항목이 생성되고, terminate로 닫을 수 있다.

---

## CTASK-3 — xterm + WS attach + 스트리밍(단일 콘솔 E2E)

### 목적
- v3의 핵심 가치인 “브라우저 터미널 스트리밍”을 단일 콘솔 기준으로 완성한다.

### 선행
- 서버 STASK-2(WS attach)

### 포함 범위
- `xterm` + `xterm-addon-fit` 도입
- WS connect → output 수신 → terminal.write
- terminal input(onData) → WS send
- resize 이벤트 → WS control message(`{type:"resize"...}`)
- 닫기: terminate + ws close + xterm dispose

### 구현 가이드
1) **xterm 초기화**
- 콘솔 선택 시 해당 콘솔에 xterm 인스턴스가 없으면 생성
- DOM mount 이후 FitAddon으로 사이즈 맞추기

2) **WS attach**
- `ws_url`로 `new WebSocket(...)`
- binaryType은 서버 구현에 맞춰 설정(보통 `arraybuffer`)
- WS message:
  - binary → string/bytes로 변환 후 `terminal.write`
  - text(JSON) → `exit/error` 처리

3) **resize**
- `ResizeObserver` 또는 window resize 이벤트에서 fit 수행
- fit 결과 cols/rows를 서버로 전송

4) **오류 처리**
- WS open 실패/close 시 status를 `ERROR` 또는 `CLOSED`로 전환
- 사용자에게 재시도/닫기 선택지를 제공(재연결은 비범위이므로 재시도는 “새 세션 생성”을 권장)

### 테스트/검증
- Unit(가능한 범위): WS 메시지 수신 → terminal.write 호출
- Component: mock websocket으로 onData → send 호출
- E2E(권장): `echo __kkachi_test__` 입력 시 화면에 출력 확인

### 완료 기준
- 단일 콘솔에서 echo roundtrip이 재현된다.
- 닫기 시 자원이 정리되고 UI 상태가 정상 전환된다.

---

## CTASK-4 — Multi-console + 전환(연결 유지) + 정리

### 목적
- “복수 콘솔” UX를 완성한다.
- v3 비범위인 재연결을 피하기 위해, 기본 정책은 **전환 시 연결 유지**로 고정한다.

### 선행
- 서버 STASK-2 이후 가능
- 정리 정책이 STASK-3에서 고정되면 안정성이 증가

### 포함 범위
- 콘솔 복수 생성
- 좌측 리스트 전환
- 비활성 콘솔의 WS/xterm 인스턴스 유지(기본)
- 동시 콘솔 수 상한(권장: 5)
- 페이지 언마운트 시 정리(WS close; 필요 시 terminate)

### 구현 가이드
1) **전환 정책**
- 클릭 전환은 UI 레벨에서 활성 표시만 바꾸고, 각 콘솔 WS는 유지
- 활성 콘솔만 화면에 표시(나머지는 숨김)

2) **리소스 가드레일**
- 상한 도달 시 새 콘솔 생성 차단 + 안내 메시지

3) **정리**
- console 닫기: terminate → ws close → xterm dispose → 상태 제거
- 페이지 종료: 모든 WS close(서버가 disconnect 즉시 종료 정책이면 충분)

### 테스트/검증
- Unit: add/select/remove 전이
- Component: 2콘솔 생성 → 전환 → 각각 독립적으로 상태 유지

### 완료 기준
- 복수 콘솔 생성/전환이 정상 동작한다.
- 닫기/페이지 종료 시 콘솔 자원이 정리된다.

---

## CTASK-5 — DnD reorder + (선택)정렬 저장 + Auth 토큰 대응 + UX/테스트 마감

### 목적
- 사용자가 요구한 “드래그&드롭 순서 변경”을 구현하고, v3 보안 스캐폴딩(토큰 auth)에 클라이언트를 대응시킨다.
- 릴리즈 가능한 수준으로 UX/테스트를 마감한다.

### 선행
- DnD 자체는 독립 진행 가능
- auth 토큰 전달 방식은 서버 STASK-5에서 확정 후 반영

### 포함 범위
- DnD reorder(좌측 콘솔 리스트)
- (선택) 정렬 상태 localStorage 저장
  - 단, v3는 세션 복구 비범위이므로 “정렬 선호” 수준만 저장하고 세션 자동 복구는 하지 않는다.
- Auth 토큰 지원
  - HTTP: `Authorization: Bearer <token>`
  - WS: 서버 선택 방식에 맞춰 token 전달(query/cookie 중 하나)
- 에러/빈 상태/로딩 polish
- E2E 스모크 테스트 보강

### 구현 가이드
1) **DnD**
- 라이브러리 추천: `@dnd-kit/sortable`
- 주의: reorder 시 console record의 key(consoleId) 유지 → 터미널 re-mount 최소화

2) **정렬 저장(선택)**
- 저장 대상: `consoleId` 순서(또는 workspaceId 기반)
- 새로고침 시:
  - 기존 세션을 복구하지 않으며(비범위), 새 세션 생성 시 기본 정렬을 재적용하는 정도로 제한

3) **Auth 토큰**
- 토큰 공급 경로(권장): `.env` 또는 런타임 config
- WS token 전달 방식은 STASK-5 문서에 맞춘다.

### 테스트/검증
- Unit: reorder 알고리즘
- Component: DnD 후 DOM 순서 변경 확인
- E2E(권장)
  - 2콘솔 생성 → reorder → 선택 유지
  - (auth on) 토큰 없으면 실패, 토큰 있으면 성공

### 완료 기준
- DnD reorder가 안정적으로 동작한다.
- auth on/off 모두에서 클라이언트가 올바르게 동작한다.
- 최소 E2E 스모크가 CI 또는 로컬에서 재현 가능하다.

