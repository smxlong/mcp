#!/usr/bin/env python3
"""
Example 4: Knowledge Graph and Semantic Relationships

This example demonstrates:
- Graph-like data structures using JSON
- Entity-relationship modeling
- Bi-directional link management
- Path traversal and reachability queries
- Subgraph extraction
- Semantic queries across relationships
"""

import sys
import os
import json

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from mt_common import MTClient, print_header, print_result


def main():
    """Run the knowledge graph example."""
    
    print_header("Example 4: Knowledge Graph & Semantic Relationships")
    
    output_dir = os.path.join(
        os.path.dirname(__file__), 
        'output', 
        'example4_knowledge_graph'
    )
    
    with MTClient(output_dir) as client:
        print("✓ Connected to MT server")
        print(f"✓ Data directory: {output_dir}")
        
        # Step 1: Create knowledge graph structure
        print_header("Step 1: Initialize Knowledge Graph Structure")
        
        initial_structure = {
            "entities": {},
            "relationships": [],
            "indexes": {
                "by_type": {},
                "by_tag": {}
            },
            "metadata": {
                "schema_version": "1.0",
                "created": "2024-01-15"
            }
        }
        
        result = client.call_tool(
            operation="create_tree",
            tree="knowledge",
            data=initial_structure
        )
        print_result("Created knowledge graph", client.extract_result(result))
        
        # Step 2: Add entities (nodes) in batch
        print_header("Step 2: Add Entities (Graph Nodes)")
        
        entities = {
            "person:alice": {
                "type": "person",
                "name": "Alice Johnson",
                "role": "Software Engineer",
                "skills": ["Python", "Go", "ML"],
                "location": "San Francisco"
            },
            "person:bob": {
                "type": "person",
                "name": "Bob Smith",
                "role": "Data Scientist",
                "skills": ["Python", "R", "Statistics"],
                "location": "New York"
            },
            "person:charlie": {
                "type": "person",
                "name": "Charlie Davis",
                "role": "DevOps Engineer",
                "skills": ["Docker", "Kubernetes", "AWS"],
                "location": "Seattle"
            },
            "project:ml_platform": {
                "type": "project",
                "name": "ML Platform",
                "status": "active",
                "tech_stack": ["Python", "TensorFlow", "Kubernetes"],
                "priority": "high"
            },
            "project:api_gateway": {
                "type": "project",
                "name": "API Gateway",
                "status": "active",
                "tech_stack": ["Go", "Docker", "PostgreSQL"],
                "priority": "medium"
            },
            "skill:python": {
                "type": "skill",
                "name": "Python",
                "category": "programming_language",
                "demand": "high"
            },
            "skill:kubernetes": {
                "type": "skill",
                "name": "Kubernetes",
                "category": "devops",
                "demand": "high"
            }
        }
        
        # Add all entities
        batch_ops = []
        for entity_id, entity_data in entities.items():
            batch_ops.append({
                "operation": "set",
                "tree": "knowledge",
                "path": f".entities[\"{entity_id}\"]",
                "value": entity_data
            })
        
        result = client.call_batch(batch_ops)
        print_result("Added entities", client.extract_result(result))
        
        # Step 3: Add relationships (edges)
        print_header("Step 3: Add Relationships (Graph Edges)")
        
        relationships = [
            {"from": "person:alice", "to": "project:ml_platform", "type": "works_on", "role": "lead"},
            {"from": "person:bob", "to": "project:ml_platform", "type": "works_on", "role": "contributor"},
            {"from": "person:charlie", "to": "project:api_gateway", "type": "works_on", "role": "lead"},
            {"from": "person:alice", "to": "skill:python", "type": "has_skill", "level": "expert"},
            {"from": "person:bob", "to": "skill:python", "type": "has_skill", "level": "expert"},
            {"from": "person:charlie", "to": "skill:kubernetes", "type": "has_skill", "level": "advanced"},
            {"from": "project:ml_platform", "to": "skill:python", "type": "requires_skill", "importance": "critical"},
            {"from": "project:ml_platform", "to": "skill:kubernetes", "type": "requires_skill", "importance": "high"},
            {"from": "person:alice", "to": "person:bob", "type": "collaborates_with", "frequency": "daily"},
            {"from": "person:bob", "to": "person:alice", "type": "collaborates_with", "frequency": "daily"},
        ]
        
        batch_ops = []
        for rel in relationships:
            batch_ops.append({
                "operation": "append",
                "tree": "knowledge",
                "path": ".relationships",
                "value": rel
            })
        
        result = client.call_batch(batch_ops)
        print_result("Added relationships", client.extract_result(result))
        
        # Step 4: Build indexes for efficient querying
        print_header("Step 4: Build Indexes for Fast Lookups")
        
        # Index by entity type
        result = client.call_tool(
            operation="transform",
            tree="knowledge",
            path=".indexes.by_type",
            source_path=".entities",
            filter="""
                to_entries | 
                group_by(.value.type) | 
                map({
                    (.[0].value.type): [.[] | .key]
                }) | 
                add
            """
        )
        print_result("Created type index", client.extract_result(result))
        
        # Index relationships by type
        result = client.call_tool(
            operation="transform",
            tree="knowledge",
            path=".indexes.by_relationship_type",
            source_path=".relationships",
            filter="""
                group_by(.type) | 
                map({
                    (.[0].type): .
                }) | 
                add
            """
        )
        print_result("Created relationship index", client.extract_result(result))
        
        # Step 5: Semantic queries
        print_header("Step 5: Semantic Queries Across the Graph")
        
        queries = [
            {
                "operation": "query",
                "tree": "knowledge",
                "filter": ".entities | to_entries | map(select(.value.type == \"person\")) | map(.value.name)",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "knowledge",
                "filter": """.relationships | 
                    map(select(.type == "works_on")) | 
                    map({person: .from, project: .to, role: .role})""",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "knowledge",
                "filter": """.relationships | 
                    map(select(.type == "has_skill" and .level == "expert")) | 
                    group_by(.from) | 
                    map({
                        person: .[0].from,
                        expert_skills: [.[] | .to]
                    })""",
                "verbatim": True
            }
        ]
        
        result = client.call_batch(queries)
        batch_data = client.extract_result(result)
        
        print("Graph Query Results:")
        for i, query_result in enumerate(batch_data.get("batch_results", [])):
            print(f"\nQuery {i+1}:")
            print(json.dumps(query_result.get("data"), indent=2))
        
        # Step 6: Path traversal - find connections
        print_header("Step 6: Path Traversal and Connection Discovery")
        
        # Find all projects that require Python
        result = client.call_tool(
            operation="query",
            tree="knowledge",
            filter="""
                .relationships | 
                map(select(.type == "requires_skill" and (.to | endswith("python")))) | 
                map(.from)
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Projects requiring Python", data.get("result"))
        
        # Find people who have skills required by a project
        result = client.call_tool(
            operation="query",
            tree="knowledge",
            filter="""
                . as $root |
                ($root.relationships | map(select(.type == "requires_skill" and .from == "project:ml_platform")) | map(.to)) as $required |
                $root.relationships | 
                map(select(.type == "has_skill" and ([.to] | inside($required)))) |
                map({person: .from, skill: .to, level: .level})
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("People with skills needed for ML Platform", data.get("result"))
        
        # Step 7: Subgraph extraction
        print_header("Step 7: Extract Subgraph")
        
        # Extract everything related to Alice
        result = client.call_tool(
            operation="query",
            tree="knowledge",
            filter="""
                . as $root |
                {
                    entity: $root.entities["person:alice"],
                    relationships: ($root.relationships | map(select(.from == "person:alice" or .to == "person:alice"))),
                    related_entities: (
                        ($root.relationships | 
                         map(select(.from == "person:alice" or .to == "person:alice")) | 
                         map(.from, .to) | 
                         unique | 
                         map(select(. != "person:alice"))) as $related_ids |
                        $root.entities | to_entries | map(select([.key] | inside($related_ids))) | from_entries
                    )
                }
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Alice's subgraph", data.get("result"))
        
        # Step 8: Aggregation and analytics
        print_header("Step 8: Graph Analytics")
        
        analytics_queries = [
            {
                "operation": "query",
                "tree": "knowledge",
                "filter": """.relationships | 
                    group_by(.type) | 
                    map({type: .[0].type, count: length})""",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "knowledge",
                "filter": """.relationships | 
                    map(select(.type == "has_skill")) | 
                    group_by(.to) | 
                    map({skill: .[0].to, person_count: length}) | 
                    sort_by(.person_count) | 
                    reverse""",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "knowledge",
                "filter": """.relationships | 
                    map(.from) | 
                    group_by(.) | 
                    map({entity: .[0], outgoing_edges: length}) | 
                    sort_by(.outgoing_edges) | 
                    reverse | 
                    .[0:5]""",
                "verbatim": True
            }
        ]
        
        result = client.call_batch(analytics_queries)
        batch_data = client.extract_result(result)
        
        print("Graph Analytics:")
        analytics_names = ["Relationship type distribution", "Skills by popularity", "Most connected entities"]
        for i, query_result in enumerate(batch_data.get("batch_results", [])):
            print(f"\n{analytics_names[i]}:")
            print(json.dumps(query_result.get("data"), indent=2))
        
        # Step 9: Update graph - add new connection
        print_header("Step 9: Dynamic Graph Updates")
        
        # Get current entities, add Diana, and set back
        result = client.call_tool(
            operation="get",
            tree="knowledge",
            path=".entities"
        )
        entities = client.extract_result(result).get("data", {})
        entities["person:diana"] = {
            "type": "person",
            "name": "Diana Martinez",
            "role": "ML Engineer",
            "skills": ["Python", "TensorFlow", "ML"],
            "location": "Austin"
        }
        
        result = client.call_tool(
            operation="set",
            tree="knowledge",
            path=".entities",
            value=entities
        )
        print_result("Added Diana entity", client.extract_result(result))
        
        # Add relationships
        new_rels = [
            {"from": "person:diana", "to": "project:ml_platform", "type": "works_on", "role": "contributor"},
            {"from": "person:diana", "to": "skill:python", "type": "has_skill", "level": "expert"},
            {"from": "person:diana", "to": "person:alice", "type": "reports_to"}
        ]
        
        batch_ops = []
        for rel in new_rels:
            batch_ops.append({
                "operation": "append",
                "tree": "knowledge",
                "path": ".relationships",
                "value": rel
            })
        
        result = client.call_batch(batch_ops)
        print_result("Added Diana's relationships", client.extract_result(result))
        
        # Query updated graph
        result = client.call_tool(
            operation="query",
            tree="knowledge",
            filter=""".relationships | 
                map(select(.from == "person:diana" or .to == "person:diana")) | 
                length""",
            verbatim=True
        )
        data = client.extract_result(result)
        print(f"\nDiana now has {data.get('result')} relationships in the graph")
        
        # Clean up
        print_header("Complete!")
        result = client.call_tool(operation="delete_tree", tree="knowledge")
        print_result("Cleaned up", client.extract_result(result))
        
        print("\n" + "="*60)
        print("Key Concepts Demonstrated:")
        print("="*60)
        print("✓ Graph data modeling with entities and relationships")
        print("✓ Entity-relationship patterns in JSON")
        print("✓ Index structures for optimized lookups")
        print("✓ Semantic queries across graph connections")
        print("✓ Path traversal and reachability analysis")
        print("✓ Subgraph extraction based on entity")
        print("✓ Graph analytics (degree centrality, popularity)")
        print("✓ Dynamic graph updates and mutations")
        print("✓ Complex jq filters for graph traversal")
        print("✓ Group-by operations on relationships")


if __name__ == "__main__":
    main()
