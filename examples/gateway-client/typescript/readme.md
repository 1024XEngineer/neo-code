# NeoCode Gateway TypeScript Client Example

This is a minimal example of a TypeScript client for the NeoCode Gateway.

## Installation

```bash
npm install
```

## Usage

### Basic RPC Call

```typescript
import { NeoCodeGatewayClient } from './src/client';

const client = new NeoCodeGatewayClient({
  baseURL: 'http://localhost:8080/api/v1',
  token: 'your-auth-token', // Optional
});

// Ping the gateway
async function pingGateway() {
  try {
    const result = await client.rpc('gateway.ping');
    console.log('Ping result:', result);
  } catch (error) {
    console.error('Ping failed:', error);
  }
}

pingGateway();
```

### Subscribe to Server-Sent Events

```typescript
import { NeoCodeGatewayClient } from './src/client';

const client = new NeoCodeGatewayClient({
  baseURL: 'http://localhost:8080/api/v1'
});

// Subscribe to events
const unsubscribe = client.subscribeSSE(
  (data) => {
    console.log('Received event:', data);
  },
  (error) => {
    console.error('Event subscription error:', error);
  }
);

// Unsubscribe when needed
// unsubscribe();
```

## API

### `new NeoCodeGatewayClient(options)`

Creates a new client instance.

**Options:**
- `baseURL`: The base URL of the NeoCode Gateway
- `token` (optional): Authentication token

### `rpc(method, params?)`

Makes an RPC call to the gateway.

**Parameters:**
- `method`: The RPC method name
- `params` (optional): Parameters for the method

**Returns:** A promise that resolves to the method result

### `subscribeSSE(onMessage, onError?)`

Subscribes to Server-Sent Events from the gateway.

**Parameters:**
- `onMessage`: Callback function to handle incoming messages
- `onError` (optional): Callback function to handle errors

**Returns:** A function to close the connection