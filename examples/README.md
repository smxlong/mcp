# MT Server Examples

This directory contains command-line examples demonstrating how to use the Memory Tree (MT) server via stdio transport.

## Prerequisites

- Python 3.7+
- MT server binary built in `../server/mt/mt`

## Common Module

`mt_common.py` - Provides the `MTClient` class for communicating with the MT server via JSON-RPC over stdio. This handles:
- Process management (starting/stopping the MT server)
- JSON-RPC protocol communication
- Message ID tracking
- Response parsing

## Examples

### Example 1: Basic Batch Operations (`example1_basic.py`)

Demonstrates fundamental batch operations:
- Creating a tree with initial data
- Using batch operations to set multiple values at once
- Querying complete tree data
- Using jq filters to query specific paths
- Listing available trees
- Cleaning up resources

**Run it:**
```bash
cd examples
python3 example1_basic.py
```

**Output:** Creates a `project` tree in `output/example1_basic/` with metadata, tags, and author information.

### Example 2: Hierarchical Data Organization (`example2_hierarchical.py`)

Demonstrates sophisticated hierarchy management:
- Multi-level hierarchical structures (workspace → projects → tasks → subtasks)
- Efficient batch construction of complex hierarchies using append
- Navigation with jq path expressions
- Filtering with select() and conditional logic
- Aggregation functions (add, length, unique)
- Complex nested queries with object construction
- Data transformation for alternative views (e.g., by-assignee view)
- Group-by operations for data reorganization

**Key Concepts:** Hierarchical data modeling, complex jq queries, data transformation

### Example 3: Time-Series Data & Window Management (`example3_timeseries.py`)

Demonstrates time-series patterns:
- Time-series data structures with bounded memory
- Window management with append/prepend operations (keep last N items)
- Batch ingestion for high-throughput scenarios
- Statistical aggregations (avg, min, max)
- Group-by operations on nested tags
- Rolling calculations and trend analysis
- Alert condition detection with filtering
- Reverse chronological ordering with prepend
- Transform operations for derived metrics

**Key Concepts:** Windowed arrays, metrics aggregation, trend detection

### Example 4: Knowledge Graph & Semantic Relationships (`example4_knowledge_graph.py`)

Demonstrates graph data modeling:
- Entity-relationship patterns (nodes and edges in JSON)
- Batch entity and relationship creation
- Index building for optimized type-based lookups
- Semantic queries across graph connections
- Path traversal and reachability queries
- Subgraph extraction centered on entities
- Graph analytics (degree centrality, relationship distribution)
- Dynamic graph updates (adding entities and relationships)
- Complex multi-hop jq filters for graph traversal
- Group-by operations on relationships

**Key Concepts:** Graph structures, entity-relationships, semantic queries

### Example 5: Event Sourcing & State Machines (`example5_event_sourcing.py`)

Demonstrates event-driven architecture:
- Event sourcing with immutable event logs
- State reconstruction from event streams
- State machine modeling and transition validation
- Point-in-time queries for historical state
- Event aggregation and analytics
- Complete audit trails and history tracking
- Event replay and pattern detection
- Temporal queries across time ranges
- Lifecycle duration analysis

**Key Concepts:** Event sourcing, immutable logs, temporal queries, state machines

### Example 6: Multi-Dimensional Analysis (`example6_multidimensional.py`)

Demonstrates OLAP-style analytics:
- Multi-dimensional data cube modeling
- Fact and dimension table structures
- Slice operations (single dimension filter)
- Dice operations (multi-dimension filter)
- Roll-up aggregations (to higher level)
- Drill-down analysis (to detailed level)
- Pivot operations (cross-tabulation)
- Pre-computed aggregations for performance
- Temporal dimension analysis
- Complex multi-measure analytics

**Key Concepts:** Data cubes, OLAP operations, dimensional modeling

## Output Directory Structure

Each example saves its tree data to a dedicated subdirectory under `output/`:

```
output/
├── example1_basic/
│   └── trees/
│       └── *.json  (tree data files)
├── example2_*/
│   └── trees/
└── ...
```

This isolation ensures examples don't interfere with each other.

## Creating Your Own Examples

1. Import the common module:
```python
from mt_common import MTClient, print_header, print_result
```

2. Create a dedicated output directory:
```python
output_dir = os.path.join(
    os.path.dirname(__file__), 
    'output', 
    'my_example_name'
)
```

3. Use the MTClient context manager:
```python
with MTClient(output_dir) as client:
    # Your operations here
    result = client.call_tool(
        operation="create_tree",
        tree="mytree",
        data={"key": "value"}
    )
```

4. Extract and display results:
```python
data = client.extract_result(result)
print_result("My Operation", data)
```

## Batch Operations

Batch operations allow you to execute multiple operations in a single request:

```python
batch_ops = [
    {
        "operation": "set",
        "tree": "mytree",
        "path": ".field1",
        "value": "value1"
    },
    {
        "operation": "set",
        "tree": "mytree",
        "path": ".field2",
        "value": "value2"
    }
]

result = client.call_batch(
    operations=batch_ops,
    continue_after_errors=False  # Stop on first error
)
```

### Verbatim Results

By default, batch operations only return summary information. To get the actual data back for specific operations, set `verbatim: true`:

```python
batch_ops = [
    {
        "operation": "query",
        "tree": "mytree",
        "filter": ".somedata",
        "verbatim": True  # Include result in response
    }
]
```

## Available Operations

- `create_tree` - Create a new tree with initial data
- `delete_tree` - Delete a tree
- `list_trees` - List all available trees
- `get` - Get data from a path
- `set` - Set data at a path
- `delete` - Delete data at a path
- `append` - Append to an array
- `prepend` - Prepend to an array
- `transform` - Transform data using jq filter
- `query` - Query data using jq filter
- `gemini_search` - Perform AI-powered search (requires API key)

## Troubleshooting

**Server won't start:** Ensure the MT binary is built:
```bash
cd ..
go build -o server/mt/mt ./server/mt
```

**Permission denied:** Make examples executable:
```bash
chmod +x example*.py
```

**Data not persisting:** Check that `DATA_DIR` environment variable points to a writable directory. The `MTClient` handles this automatically but uses `SAVE_MODE=immediate` by default.
