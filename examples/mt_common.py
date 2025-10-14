"""
Common utilities for MCP Memory Tree examples.

This module provides reusable functions for interacting with the MT server
via stdio transport protocol.
"""

import json
import subprocess
import sys
import os
from typing import Any, Dict, List, Optional, Union


class MTClient:
    """Client for interacting with the MCP Memory Tree server."""
    
    def __init__(self, data_dir: str, save_mode: str = "immediate"):
        """
        Initialize the MT client.
        
        Args:
            data_dir: Directory where tree data will be stored
            save_mode: Save mode for the server ("immediate" or "periodic")
        """
        self.data_dir = os.path.abspath(data_dir)
        self.save_mode = save_mode
        self.process = None
        self.message_id = 0
        
        # Ensure output directory exists
        os.makedirs(self.data_dir, exist_ok=True)
        
    def __enter__(self):
        """Start the MT server process."""
        env = os.environ.copy()
        env['DATA_DIR'] = self.data_dir
        env['SAVE_MODE'] = self.save_mode
        
        # Start the MT server
        server_path = os.path.join(
            os.path.dirname(__file__), 
            '..', 'server', 'mt', 'mt'
        )
        
        self.process = subprocess.Popen(
            [server_path],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
            text=True,
            bufsize=1
        )
        
        # Initialize the connection
        self._send_initialize()
        
        return self
        
    def __exit__(self, exc_type, exc_val, exc_tb):
        """Stop the MT server process."""
        if self.process:
            self.process.terminate()
            self.process.wait(timeout=5)
            
    def _get_next_id(self) -> int:
        """Get next message ID."""
        self.message_id += 1
        return self.message_id
        
    def _send_message(self, method: str, params: Dict[str, Any]) -> Dict[str, Any]:
        """
        Send a JSON-RPC message to the server.
        
        Args:
            method: The method to call
            params: Parameters for the method
            
        Returns:
            The response from the server
        """
        message = {
            "jsonrpc": "2.0",
            "id": self._get_next_id(),
            "method": method,
            "params": params
        }
        
        # Send the message
        message_str = json.dumps(message) + '\n'
        if self.process and self.process.stdin:
            self.process.stdin.write(message_str)
            self.process.stdin.flush()
        
        # Read the response
        if self.process and self.process.stdout:
            response_str = self.process.stdout.readline()
            if not response_str:
                # Check for errors on stderr
                if self.process.stderr:
                    stderr_data = self.process.stderr.read()
                    if stderr_data:
                        raise RuntimeError(f"No response from server. Stderr: {stderr_data}")
                raise RuntimeError("No response from server")
            
            return json.loads(response_str)
        else:
            raise RuntimeError("Process not initialized")
        
    def _send_initialize(self):
        """Send the initialize request to establish the connection."""
        response = self._send_message("initialize", {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {
                "name": "mt-examples",
                "version": "1.0.0"
            }
        })
        
        if "error" in response:
            raise RuntimeError(f"Initialize failed: {response['error']}")
            
        # Note: notifications/initialized doesn't expect a response
        # so we don't wait for one
        
    def call_tool(self, operation: str, **kwargs) -> Dict[str, Any]:
        """
        Call the mt tool with a single operation.
        
        Args:
            operation: The operation to perform
            **kwargs: Additional parameters for the operation
            
        Returns:
            The result from the tool
        """
        params = {
            "name": "mt",
            "arguments": {
                "operation": operation,
                **kwargs
            }
        }
        
        response = self._send_message("tools/call", params)
        
        if "error" in response:
            raise RuntimeError(f"Tool call failed: {response['error']}")
            
        return response.get("result", {})
        
    def call_batch(self, operations: List[Dict[str, Any]], 
                   continue_after_errors: bool = False) -> Dict[str, Any]:
        """
        Call the mt tool with a batch of operations.
        
        Args:
            operations: List of operations to perform
            continue_after_errors: Whether to continue after errors
            
        Returns:
            The result from the batch operation
        """
        params = {
            "name": "mt",
            "arguments": {
                "operations": operations,
                "continue_after_errors": continue_after_errors
            }
        }
        
        response = self._send_message("tools/call", params)
        
        if "error" in response:
            raise RuntimeError(f"Batch call failed: {response['error']}")
            
        return response.get("result", {})
        
    def extract_result(self, response: Dict[str, Any]) -> Any:
        """
        Extract the actual result data from a tool response.
        
        Args:
            response: The response from a tool call
            
        Returns:
            The extracted result data
        """
        if "content" in response:
            for content_item in response["content"]:
                if content_item.get("type") == "text":
                    return json.loads(content_item["text"])
        return response


def print_result(label: str, result: Any):
    """
    Pretty print a result with a label.
    
    Args:
        label: Label for the result
        result: The result to print
    """
    print(f"\n{'='*60}")
    print(f"{label}")
    print(f"{'='*60}")
    print(json.dumps(result, indent=2))
    print()


def print_header(title: str):
    """
    Print a formatted header.
    
    Args:
        title: The header title
    """
    print(f"\n{'#'*60}")
    print(f"# {title}")
    print(f"{'#'*60}\n")
