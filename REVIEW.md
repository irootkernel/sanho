# CTASK-3 구현 리뷰 결과

> xterm + WS attach + 스트리밍(단일 콘솔 E2E) 구현 검토

---

## 요약

CTASK-3의 핵심 요구사항 대부분이 잘 구현되었습니다. **xterm 초기화, WebSocket 연결, 입출력 스트리밍, resize 이벤트 처리, 자원 정리**가 모두 포함되어 있으며, 테스트 커버리지도 E2E 수준까지 확보되었습니다.

그러나 **1개의 Critical 버그**와 **몇 가지 개선 권장 사항**이 발견되었습니다.

---

## Critical 버그 🔴

### 1. `pty.ts` 들여쓰기/문법 오류

`web/src/api/pty.ts` (Lines 38-52) 파일에 **심각한 들여쓰기 오류**가 있습니다:

```typescript
// 현재 (Lines 38-52)
if (!response.ok) {
    let message = `Server returned ${response.status}: ${response.statusText}`;
    try {
        const errorData = await response.json();
        if (errorData.message) message = errorData.message;
        else if (errorData.error) message = errorData.error;
    } catch {
        // Ignore parse error
    }
            throw new ApiError(message, response.status);  // <-- 잘못된 들여쓰기
        }                                                   // <-- if 블록 밖에서 닫힘
    
        const data: CreateSessionResponse = await response.json();  // <-- if 블록 밖
        return data;                                                // <-- if 블록 밖
    };                                                              // <-- 함수 끝이 두 번
```

이 코드는 **빌드는 되지만** 로직이 완전히 깨져 있습니다:
- `throw`가 `if (!response.ok)` 블록 내부에서 실행되지 않을 수 있음
- 성공 응답 처리 코드가 에러 블록 밖에 있어 정상 동작하지만, 코드 구조가 매우 혼란스럽습니다.

**수정 필요:**
```typescript
if (!response.ok) {
    let message = `Server returned ${response.status}: ${response.statusText}`;
    try {
        const errorData = await response.json();
        if (errorData.message) message = errorData.message;
        else if (errorData.error) message = errorData.error;
    } catch {
        // Ignore parse error
    }
    throw new ApiError(message, response.status);
}

const data: CreateSessionResponse = await response.json();
return data;
```

---

## 구현 검증 ✅

| 요구사항 | 구현 상태 | 위치 |
|---------|:--------:|------|
| xterm + xterm-addon-fit 도입 | ✅ | `TerminalPane.tsx:L46-71` |
| WS connect → output 수신 → terminal.write | ✅ | `TerminalPane.tsx:L96-157` |
| terminal input(onData) → WS send | ✅ | `TerminalPane.tsx:L76-80` |
| resize 이벤트 → WS control message | ✅ | `TerminalPane.tsx:L28-37`, `L159-170` |
| 닫기: terminate + ws close + xterm dispose | ✅ | `TerminalPane.tsx:L180-193` |
| 오류 처리 (WS open 실패/close → ERROR/CLOSED) | ✅ | `TerminalPane.tsx:L115-127` |
| binaryType 설정 | ✅ | `pty.ts:L57` |
| exit/error JSON 메시지 처리 | ✅ | `TerminalPane.tsx:L132-142` |

---

## 추가 변경 사항 (CTASK-3 범위 외)

CTASK-4 범위의 기능들이 함께 구현되었습니다 (리뷰 대상 참고):

| 기능 | 상태 | 비고 |
|-----|:----:|-----|
| Multi-console 렌더링 | 🔵 | `TerminalPage.tsx:L79-93` |
| Console 5개 상한 | 🔵 | `useTerminalStore.ts:L3`, `L21-25` |
| 페이지 언마운트 시 WS close | 🔵 | `TerminalPage.tsx:L16-21` |

🔵 = CTASK-4 범위이지만 함께 구현됨

---

## 개선 권장 사항

### 1. Console title 표시 개선
현재 `workspace_id.split(':').pop()?.split('/').pop()` 로직으로 title을 추출합니다. workspace_id 형식이 변경되면 깨질 수 있으므로, 서버에서 `title`을 명시적으로 반환하거나 fallback을 더 robust하게 처리하는 것이 좋습니다.

### 2. ResizeObserver 디바운싱
`syncSizeWithServer()`가 resize마다 즉시 호출됩니다. 빠른 resize 시 WS 메시지가 과도하게 전송될 수 있으므로 **debounce** 적용을 권장합니다.

### 3. 서버 config 변경 (참고)
`internal/pty/config.go`의 shell 감지 로직이 개선되었습니다 (`$SHELL` 환경변수 우선 사용). 이는 좋은 개선이지만 CTASK-3 scope 외입니다.

---

## 테스트 커버리지

| 테스트 유형 | 구현 | 비고 |
|-----------|:---:|-----|
| TerminalPane unit test | ✅ | `TerminalPane.test.tsx` |
| E2E: console lifecycle | ✅ | `terminal.spec.ts:L53-76` |
| E2E: multi-console switching | ✅ | `terminal.spec.ts:L78-108` |
| E2E: 5-console limit | ✅ | `terminal.spec.ts:L110-127` |
| E2E: error handling | ✅ | `terminal.spec.ts:L129-153` |

---

## 결론

| 평가 | 결과 |
|-----|:----:|
| CTASK-3 요구사항 충족 | ✅ |
| Critical 버그 | 🔴 1개 (`pty.ts` 들여쓰기) |
| 커밋 가능 여부 | ❌ (버그 수정 필요) |
