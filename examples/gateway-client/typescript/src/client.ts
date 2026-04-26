export interface GatewayClientOptions {
  baseURL: string;
  token?: string;
}

export interface RPCRequest {
  method: string;
  params?: any;
  id: number;
}

export interface RPCResponse {
  result?: any;
  error?: {
    code: number;
    message: string;
  };
  id: number;
}

export class NeoCodeGatewayClient {
  private baseURL: string;
  private token?: string;
  private nextId: number = 1;

  constructor(options: GatewayClientOptions) {
    this.baseURL = options.baseURL;
    this.token = options.token;
  }

  /**
   * Make an RPC call to the gateway
   * @param method The RPC method name
   * @param params Optional parameters for the method
   * @returns Promise resolving to the RPC response
   */
  async rpc<T = any>(method: string, params?: any): Promise<T> {
    const id = this.nextId++;
    const request: RPCRequest = { method, params, id };

    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    const response = await fetch(`${this.baseURL}/rpc`, {
      method: 'POST',
      headers,
      body: JSON.stringify(request),
    });

    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }

    const rpcResponse: RPCResponse = await response.json();

    if (rpcResponse.error) {
      throw new Error(`RPC error: ${rpcResponse.error.message} (code: ${rpcResponse.error.code})`);
    }

    return rpcResponse.result as T;
  }

  /**
   * Subscribe to Server-Sent Events from the gateway
   * @param onMessage Callback function to handle incoming messages
   * @param onError Callback function to handle errors
   * @returns A function to close the connection
   */
  subscribeSSE(onMessage: (data: any) => void, onError?: (error: any) => void): () => void {
    const url = `${this.baseURL}/events`;
    const eventSource = new EventSource(url);

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        onMessage(data);
      } catch (err) {
        if (onError) onError(err);
      }
    };

    eventSource.onerror = (error) => {
      if (onError) onError(error);
    };

    return () => {
      eventSource.close();
    };
  }
}

export default NeoCodeGatewayClient;