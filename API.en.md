# DS2API API Reference

Language: [中文](API.md) | [English](API.en.md)

This document describes all DS2API API endpoints.

---

## Table of Contents

- [Basics](#basics)
- [OpenAI-Compatible API](#openai-compatible-api)
  - [List Models](#list-models)
  - [Chat Completions](#chat-completions)
- [Claude-Compatible API](#claude-compatible-api)
  - [Claude Model List](#claude-model-list)
  - [Claude Messages](#claude-messages)
  - [Token Counting](#token-counting)
- [Admin API](#admin-api)
  - [Login](#login)
  - [Configuration](#configuration)
  - [Account Management](#account-management)
  - [Vercel Sync](#vercel-sync)
- [Error Handling](#error-handling)
- [Examples](#examples)

---

## Basics

| Item | Description |
|-----|------|
| **Base URL** | `https://your-domain.com` or `http://localhost:5001` |
| **OpenAI auth** | `Authorization: Bearer <api-key>` |
| **Claude auth** | `x-api-key: <api-key>` |
| **Response format** | JSON |

---

## OpenAI-Compatible API

### List Models

```http
GET /v1/models
```

**Response example**:

```json
{
  "object": "list",
  "data": [
    {"id": "deepseek-chat", "object": "model", "owned_by": "deepseek"},
    {"id": "deepseek-reasoner", "object": "model", "owned_by": "deepseek"},
    {"id": "deepseek-chat-search", "object": "model", "owned_by": "deepseek"},
    {"id": "deepseek-reasoner-search", "object": "model", "owned_by": "deepseek"}
  ]
}
```

---

### Chat Completions

```http
POST /v1/chat/completions
Authorization: Bearer your-api-key
Content-Type: application/json
```

**Parameters**:

| Parameter | Type | Required | Description |
|-----|------|:----:|------|
| `model` | string | ✅ | Model name (see below) |
| `messages` | array | ✅ | Chat messages |
| `stream` | boolean | ❌ | Stream responses (default `false`) |
| `temperature` | number | ❌ | Temperature (0-2) |
| `max_tokens` | number | ❌ | Max output tokens |
| `tools` | array | ❌ | Tool definitions (Function Calling) |
| `tool_choice` | string | ❌ | Tool selection strategy |

**Supported models**:

| Model | Reasoning | Search | Notes |
|-----|:--------:|:------:|------|
| `deepseek-chat` | ❌ | ❌ | Standard chat |
| `deepseek-reasoner` | ✅ | ❌ | Reasoning mode with trace |
| `deepseek-chat-search` | ❌ | ✅ | Search enhanced |
| `deepseek-reasoner-search` | ✅ | ✅ | Reasoning + search |

**Basic request example**:

```json
{
  "model": "deepseek-chat",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello"}
  ]
}
```

**Streaming request example**:

```json
{
  "model": "deepseek-reasoner-search",
  "messages": [
    {"role": "user", "content": "What's in the news today?"}
  ],
  "stream": true
}
```

**Streaming response format** (`stream: true`):

```
data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"reasoning_content":"Let me think..."},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"content":"Based on search results..."},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"index":0,"finish_reason":"stop"}]}

data: [DONE]
```

> **Note**: Reasoning models emit `reasoning_content` with the trace.

**Non-streaming response format** (`stream: false`):

```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "created": 1738400000,
  "model": "deepseek-reasoner",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Response text",
      "reasoning_content": "Reasoning trace (reasoner only)"
    },
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 50,
    "total_tokens": 60,
    "completion_tokens_details": {
      "reasoning_tokens": 20
    }
  }
}
```

#### Tool Calling (Function Calling)

**Request example**:

```json
{
  "model": "deepseek-chat",
  "messages": [{"role": "user", "content": "What's the weather in Beijing?"}],
  "tools": [{
    "type": "function",
    "function": {
      "name": "get_weather",
      "description": "Get the weather for a city",
      "parameters": {
        "type": "object",
        "properties": {
          "location": {"type": "string", "description": "City name"}
        },
        "required": ["location"]
      }
    }
  }]
}
```

**Response example**:

```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": null,
      "tool_calls": [{
        "id": "call_xxx",
        "type": "function",
        "function": {
          "name": "get_weather",
          "arguments": "{\"location\": \"Beijing\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}
```

---

## Claude-Compatible API

### Claude Model List

```http
GET /anthropic/v1/models
```

**Response example**:

```json
{
  "object": "list",
  "data": [
    {"id": "claude-sonnet-4-20250514", "object": "model", "owned_by": "anthropic"},
    {"id": "claude-sonnet-4-20250514-fast", "object": "model", "owned_by": "anthropic"},
    {"id": "claude-sonnet-4-20250514-slow", "object": "model", "owned_by": "anthropic"}
  ]
}
```

**Model mapping**:

| Claude Model | Actual | Notes |
|------------|--------|------|
| `claude-sonnet-4-20250514` | deepseek-chat | Standard mode |
| `claude-sonnet-4-20250514-fast` | deepseek-chat | Fast mode |
| `claude-sonnet-4-20250514-slow` | deepseek-reasoner | Reasoning mode |

---

### Claude Messages

```http
POST /anthropic/v1/messages
x-api-key: your-api-key
Content-Type: application/json
anthropic-version: 2023-06-01
```

**Parameters**:

| Parameter | Type | Required | Description |
|-----|------|:----:|------|
| `model` | string | ✅ | Model name |
| `max_tokens` | integer | ✅ | Max output tokens |
| `messages` | array | ✅ | Chat messages |
| `stream` | boolean | ❌ | Stream responses (default `false`) |
| `system` | string | ❌ | System prompt |
| `temperature` | number | ❌ | Temperature |

**Request example**:

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "messages": [
    {"role": "user", "content": "Hello, please introduce yourself."}
  ]
}
```

**Non-streaming response**:

```json
{
  "id": "msg_xxx",
  "type": "message",
  "role": "assistant",
  "content": [{
    "type": "text",
    "text": "Hello! I'm an AI assistant..."
  }],
  "model": "claude-sonnet-4-20250514",
  "stop_reason": "end_turn",
  "usage": {
    "input_tokens": 10,
    "output_tokens": 50
  }
}
```

**Streaming response** (SSE):

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_xxx","type":"message","role":"assistant","model":"claude-sonnet-4-20250514"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":50}}

event: message_stop
data: {"type":"message_stop"}
```

---

### Token Counting

```http
POST /anthropic/v1/messages/count_tokens
x-api-key: your-api-key
Content-Type: application/json
```

**Request example**:

```json
{
  "model": "claude-sonnet-4-20250514",
  "messages": [
    {"role": "user", "content": "Hello"}
  ]
}
```

**Response example**:

```json
{
  "input_tokens": 5
}
```

---

## Admin API

All admin endpoints (except login) require `Authorization: Bearer <jwt-token>`.

### Login

```http
POST /admin/login
Content-Type: application/json
```

**Request body**:

```json
{
  "key": "your-admin-key"
}
```

**Response**:

```json
{
  "success": true,
  "token": "jwt-token-string",
  "expires_in": 86400
}
```

> Tokens are valid for 24 hours by default.

---

### Configuration

#### Get configuration

```http
GET /admin/config
Authorization: Bearer <jwt-token>
```

**Response**:

```json
{
  "keys": ["api-key-1", "api-key-2"],
  "accounts": [
    {
      "email": "user@example.com",
      "password": "***",
      "token": "session-token"
    }
  ]
}
```

#### Update configuration

```http
POST /admin/config
Authorization: Bearer <jwt-token>
Content-Type: application/json
```

**Request body**:

```json
{
  "keys": ["new-api-key"],
  "accounts": [...]
}
```

---

### Account Management

#### Add account

```http
POST /admin/accounts
Authorization: Bearer <jwt-token>
Content-Type: application/json
```

**Request body**:

```json
{
  "email": "user@example.com",
  "password": "password123"
}
```

#### Batch import accounts

```http
POST /admin/accounts/batch
Authorization: Bearer <jwt-token>
Content-Type: application/json
```

**Request body**:

```json
{
  "accounts": [
    {"email": "user1@example.com", "password": "pass1"},
    {"email": "user2@example.com", "password": "pass2"}
  ]
}
```

#### Test one account

```http
POST /admin/accounts/test
Authorization: Bearer <jwt-token>
Content-Type: application/json
```

**Request body**:

```json
{
  "email": "user@example.com"
}
```

#### Test all accounts

```http
POST /admin/accounts/test-all
Authorization: Bearer <jwt-token>
```

#### Queue status

```http
GET /admin/queue/status
Authorization: Bearer <jwt-token>
```

**Response**:

```json
{
  "total_accounts": 5,
  "healthy_accounts": 4,
  "queue_size": 10,
  "accounts": [
    {
      "email": "user@example.com",
      "status": "healthy",
      "last_used": "2026-02-01T12:00:00Z"
    }
  ]
}
```

---

### Vercel Sync

```http
POST /admin/vercel/sync
Authorization: Bearer <jwt-token>
Content-Type: application/json
```

**Request body** (first sync only):

```json
{
  "vercel_token": "your-vercel-token",
  "project_id": "your-project-id"
}
```

> After a successful first sync, credentials are stored for future syncs.

**Response**:

```json
{
  "success": true,
  "message": "Configuration synced to Vercel"
}
```

---

## Error Handling

All error responses follow this structure:

```json
{
  "error": {
    "message": "Error description",
    "type": "error_type",
    "code": "error_code"
  }
}
```

**Common error codes**:

| HTTP Status | Error Type | Description |
|:----------:|---------|------|
| 400 | `invalid_request_error` | Invalid request parameters |
| 401 | `authentication_error` | Missing or invalid API key |
| 403 | `permission_denied` | Insufficient permissions |
| 429 | `rate_limit_error` | Too many requests |
| 500 | `internal_error` | Internal server error |
| 503 | `service_unavailable` | No available accounts |

---

## Examples

### Python (OpenAI SDK)

```python
from openai import OpenAI

client = OpenAI(
    api_key="your-api-key",
    base_url="https://your-domain.com/v1"
)

# Basic chat
response = client.chat.completions.create(
    model="deepseek-chat",
    messages=[{"role": "user", "content": "Hello"}]
)
print(response.choices[0].message.content)

# Streaming + reasoning
for chunk in client.chat.completions.create(
    model="deepseek-reasoner",
    messages=[{"role": "user", "content": "Explain relativity"}],
    stream=True
):
    delta = chunk.choices[0].delta
    if hasattr(delta, 'reasoning_content') and delta.reasoning_content:
        print(f"[Reasoning] {delta.reasoning_content}", end="")
    if delta.content:
        print(delta.content, end="")
```

### Python (Anthropic SDK)

```python
import anthropic

client = anthropic.Anthropic(
    api_key="your-api-key",
    base_url="https://your-domain.com/anthropic"
)

response = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Hello"}]
)
print(response.content[0].text)
```

### cURL

```bash
# OpenAI format
curl https://your-domain.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "Hello"}]
  }'

# Claude format
curl https://your-domain.com/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: your-api-key" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### JavaScript / TypeScript

```javascript
// OpenAI format - streaming request
const response = await fetch('https://your-domain.com/v1/chat/completions', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Authorization': 'Bearer your-api-key'
  },
  body: JSON.stringify({
    model: 'deepseek-chat-search',
    messages: [{ role: 'user', content: 'What is in the news today?' }],
    stream: true
  })
});

const reader = response.body.getReader();
const decoder = new TextDecoder();

while (true) {
  const { done, value } = await reader.read();
  if (done) break;
  
  const chunk = decoder.decode(value);
  const lines = chunk.split('\n').filter(line => line.startsWith('data: '));
  
  for (const line of lines) {
    const data = line.slice(6);
    if (data === '[DONE]') continue;
    
    const json = JSON.parse(data);
    const content = json.choices?.[0]?.delta?.content;
    if (content) process.stdout.write(content);
  }
}
```

### Node.js (OpenAI SDK)

```javascript
import OpenAI from 'openai';

const client = new OpenAI({
  apiKey: 'your-api-key',
  baseURL: 'https://your-domain.com/v1'
});

const stream = await client.chat.completions.create({
  model: 'deepseek-reasoner',
  messages: [{ role: 'user', content: 'Explain black holes' }],
  stream: true
});

for await (const chunk of stream) {
  const content = chunk.choices[0]?.delta?.content;
  if (content) process.stdout.write(content);
}
```
