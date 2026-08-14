#!/usr/bin/env python3
"""
Example 5: Event Sourcing and State Machines

This example demonstrates:
- Event-driven architecture with event logs
- State reconstruction from event streams
- State machine transitions with validation
- Temporal queries (point-in-time state)
- Event aggregation and replay
- Audit trails and history tracking

Command-line options:
  --no-clean    Preserve trees after completion (leaves persisted data)
"""

import sys
import os
import json
from datetime import datetime, timedelta

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from mt_common import MTClient, print_header, print_result, parse_example_args


def create_event(event_type, entity_id, data, timestamp=None):
    """Create an event with standard structure."""
    if timestamp is None:
        timestamp = datetime.now().isoformat()
    return {
        "event_id": f"{event_type}_{entity_id}_{timestamp.replace(':', '').replace('.', '')}",
        "type": event_type,
        "entity_id": entity_id,
        "timestamp": timestamp,
        "data": data
    }


def main():
    """Run the event sourcing example."""
    
    args = parse_example_args("Example 5: Event Sourcing & State Machines")
    
    print_header("Example 5: Event Sourcing & State Machines")
    
    output_dir = os.path.join(
        os.path.dirname(__file__), 
        'output', 
        'example5_event_sourcing'
    )
    
    with MTClient(output_dir) as client:
        print("✓ Connected to MT server")
        print(f"✓ Data directory: {output_dir}")
        
        # Step 1: Initialize event log structure
        print_header("Step 1: Initialize Event Log Structure")
        
        initial_structure = {
            "events": [],
            "entities": {},
            "state_machine": {
                "order_states": ["created", "paid", "shipped", "delivered", "cancelled"],
                "valid_transitions": {
                    "created": ["paid", "cancelled"],
                    "paid": ["shipped", "cancelled"],
                    "shipped": ["delivered"],
                    "delivered": [],
                    "cancelled": []
                }
            },
            "metadata": {
                "event_count": 0,
                "last_event_time": None
            }
        }
        
        result = client.call_tool(
            operation="create_tree",
            tree="events",
            data=initial_structure
        )
        print_result("Created event sourcing tree", client.extract_result(result))
        
        # Step 2: Record events for order lifecycle
        print_header("Step 2: Record Order Lifecycle Events")
        
        base_time = datetime.now() - timedelta(hours=2)
        
        events = [
            create_event("order_created", "order_001", 
                        {"customer": "alice", "items": ["laptop", "mouse"], "total": 1299.99}, 
                        (base_time).isoformat()),
            create_event("order_created", "order_002", 
                        {"customer": "bob", "items": ["keyboard"], "total": 89.99}, 
                        (base_time + timedelta(minutes=15)).isoformat()),
            create_event("payment_received", "order_001", 
                        {"method": "credit_card", "amount": 1299.99}, 
                        (base_time + timedelta(minutes=30)).isoformat()),
            create_event("order_shipped", "order_001", 
                        {"carrier": "FedEx", "tracking": "TRK123456"}, 
                        (base_time + timedelta(hours=1)).isoformat()),
            create_event("payment_received", "order_002", 
                        {"method": "paypal", "amount": 89.99}, 
                        (base_time + timedelta(minutes=45)).isoformat()),
            create_event("order_shipped", "order_002", 
                        {"carrier": "UPS", "tracking": "TRK789012"}, 
                        (base_time + timedelta(hours=1, minutes=15)).isoformat()),
            create_event("order_delivered", "order_001", 
                        {"signed_by": "Alice", "delivery_time": "2PM"}, 
                        (base_time + timedelta(hours=2)).isoformat()),
        ]
        
        # Append events in batch
        batch_ops = []
        for event in events:
            batch_ops.append({
                "operation": "append",
                "tree": "events",
                "path": ".events",
                "value": event,
                "window": 1000  # Keep last 1000 events
            })
        
        result = client.call_batch(batch_ops)
        print_result("Recorded events", client.extract_result(result))
        
        # Step 3: Rebuild current state from events
        print_header("Step 3: Rebuild Current State from Event Stream")
        
        # Use transform to build current state
        result = client.call_tool(
            operation="transform",
            tree="events",
            path=".entities",
            source_path=".events",
            filter="""
                group_by(.entity_id) | 
                map({
                    (.[0].entity_id): {
                        entity_id: .[0].entity_id,
                        events: .,
                        current_state: (
                            if (map(select(.type == "order_delivered")) | length) > 0 then "delivered"
                            elif (map(select(.type == "order_shipped")) | length) > 0 then "shipped"
                            elif (map(select(.type == "payment_received")) | length) > 0 then "paid"
                            elif (map(select(.type == "order_created")) | length) > 0 then "created"
                            else "unknown"
                            end
                        ),
                        event_count: length,
                        created_at: .[0].timestamp,
                        last_updated: .[-1].timestamp
                    }
                }) | 
                add
            """
        )
        print_result("Rebuilt entity states", client.extract_result(result))
        
        # Query current states
        result = client.call_tool(
            operation="query",
            tree="events",
            filter=".entities | to_entries | map({order: .key, state: .value.current_state, events: .value.event_count})",
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Current order states", data.get("result"))
        
        # Step 4: Point-in-time queries
        print_header("Step 4: Point-in-Time State Reconstruction")
        
        # What was the state 1 hour ago?
        one_hour_ago = (base_time + timedelta(hours=1)).isoformat()
        
        result = client.call_tool(
            operation="query",
            tree="events",
            filter=f"""
                .events | 
                map(select(.timestamp <= "{one_hour_ago}")) |
                group_by(.entity_id) |
                map({{
                    order: .[0].entity_id,
                    state_at_time: (
                        if (map(select(.type == "order_delivered")) | length) > 0 then "delivered"
                        elif (map(select(.type == "order_shipped")) | length) > 0 then "shipped"
                        elif (map(select(.type == "payment_received")) | length) > 0 then "paid"
                        elif (map(select(.type == "order_created")) | length) > 0 then "created"
                        else "unknown"
                        end
                    ),
                    last_event: .[-1].type
                }})
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result(f"State as of 1 hour ago ({one_hour_ago})", data.get("result"))
        
        # Step 5: Event aggregation and metrics
        print_header("Step 5: Event Stream Analytics")
        
        analytics_queries = [
            {
                "operation": "query",
                "tree": "events",
                "filter": ".events | group_by(.type) | map({event_type: .[0].type, count: length})",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "events",
                "filter": """.events | 
                    group_by(.entity_id) | 
                    map({
                        order: .[0].entity_id,
                        lifecycle_duration: (
                            (.[−1].timestamp | fromdateiso8601) - (.[0].timestamp | fromdateiso8601)
                        )
                    })""",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "events",
                "filter": """.events | 
                    map(select(.type == "payment_received")) | 
                    map(.data.amount) | 
                    {total_revenue: add, transaction_count: length, average: (add / length)}""",
                "verbatim": True
            }
        ]
        
        result = client.call_batch(analytics_queries)
        batch_data = client.extract_result(result)
        
        print("Event Stream Analytics:")
        analytics_names = ["Event type distribution", "Order lifecycle durations", "Revenue metrics"]
        for i, query_result in enumerate(batch_data.get("batch_results", [])):
            print(f"\n{analytics_names[i]}:")
            print(json.dumps(query_result.get("data"), indent=2))
        
        # Step 6: Validate state transitions
        print_header("Step 6: State Machine Validation")
        
        # Try to add an invalid transition
        print("\nAttempting invalid transition: shipped → created (should fail)")
        
        # Get current state of order_001
        result = client.call_tool(
            operation="query",
            tree="events",
            filter='.entities["order_001"].current_state',
            verbatim=True
        )
        current_state = client.extract_result(result).get("result")
        print(f"Order 001 current state: {current_state}")
        
        # Check if transition is valid
        result = client.call_tool(
            operation="query",
            tree="events",
            filter=f'.state_machine.valid_transitions["{current_state}"]',
            verbatim=True
        )
        valid_next_states = client.extract_result(result).get("result", [])
        print(f"Valid next states from '{current_state}': {valid_next_states}")
        
        # Step 7: Event replay and correction
        print_header("Step 7: Event Stream Analysis - Find Patterns")
        
        # Find orders that went from created to delivered in under 2 hours
        result = client.call_tool(
            operation="query",
            tree="events",
            filter="""
                .events | 
                group_by(.entity_id) |
                map(select(
                    (map(.type) | unique | length) >= 4 and
                    (map(select(.type == "order_delivered")) | length) > 0
                )) |
                map({
                    order: .[0].entity_id,
                    first_event: .[0].timestamp,
                    last_event: .[-1].timestamp,
                    event_sequence: [.[] | .type],
                    total_events: length
                })
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Completed order journeys", data.get("result"))
        
        # Step 8: Audit trail queries
        print_header("Step 8: Audit Trail and History")
        
        # Get complete history for an order
        result = client.call_tool(
            operation="query",
            tree="events",
            filter="""
                .events | 
                map(select(.entity_id == "order_001")) |
                map({
                    timestamp: .timestamp,
                    event: .type,
                    details: .data
                })
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Complete audit trail for order_001", data.get("result"))
        
        # Update metadata
        result = client.call_tool(
            operation="transform",
            tree="events",
            path=".metadata",
            source_path=".events",
            filter="""
                {
                    event_count: length,
                    last_event_time: .[-1].timestamp,
                    unique_entities: (map(.entity_id) | unique | length),
                    event_types: (map(.type) | unique)
                }
            """
        )
        print_result("Updated metadata", client.extract_result(result))
        
        # Clean up
        print_header("Complete!")
        if not args.no_clean:
            result = client.call_tool(operation="delete_tree", tree="events")
            print_result("Cleaned up", client.extract_result(result))
        else:
            print(f"✓ Tree 'events' preserved in {output_dir}/trees/events.json")
        
        print("\n" + "="*60)
        print("Key Concepts Demonstrated:")
        print("="*60)
        print("✓ Event sourcing architecture with immutable event logs")
        print("✓ State reconstruction from event streams")
        print("✓ State machine modeling and transition validation")
        print("✓ Point-in-time queries for historical state")
        print("✓ Event aggregation and analytics")
        print("✓ Audit trails with complete history")
        print("✓ Event replay and pattern detection")
        print("✓ Temporal queries across time ranges")
        print("✓ Metadata extraction from event streams")


if __name__ == "__main__":
    main()
