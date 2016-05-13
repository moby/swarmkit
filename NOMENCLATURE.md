# Nomenclature

To keep track of the various software components in Swarm, this document
defines various aspects of the Swarm system, as referenced in this code base.

Several of these definitions may be a part of the product, while others are
simply for communicating about backend components. Where this distinction is
important, it will be called out.

## Overview

There are several moving parts in a swarm cluster. This section attempts to
define the high-level aspects that can provide context to the specifics.

To begin, we'll define the concept of a _cluster_.

### Cluster

A _Cluster_ is made up of an organized set of Docker engines configured in a
manner to allow the dispatch of _services_.

### Node

A _Node_ refers to an active member in a cluster. Nodes can execute work and
act as a cluster manager.

### Manager

A manager accepts _services_ defined by users through the Cluster API. When a
valid _service_ is provided, the manager will generate tasks, allocate resources
and dispatch _tasks_ to an available _node_.

_Managers_ operate in a coordinated group, organized via the Raft protocol.
When quorum is available, a leader will be elected to handle all API requests.

#### Orchestrator

The _Orchestrator_ ensures that services have the appropriate set of tasks
running in the cluster, according the service configuration and polices.

#### Allocator

The allocator allocates resources, such as volumes and networks to tasks, as required.

#### Scheduler

The _scheduler_ assigns to tasks to available nodes.

#### Dispatcher

The _Dispatcher_ directly handles all agent connections. This includes
registration, session management, and notification of task assignment.

### Worker

A _Worker_ is a complete engine joined to a _cluster_. It receives and executes
_tasks_ while reporting on their status. _Tasks_ include definitions of
container runtimes.

A worker's _agent_ coordinates the receipt of task assignments and ensures status
is correctly reported to a _dispatcher_.

#### Engine

The _engine_ is shorthand for the _Docker Engine_. It runs containers
distributed via _tasks_.

#### Agent

The _agent_ coordinates the dispatch of work for a _worker_. The _agent_
maintains a connection to the _dispatcher_, waiting for the current set of
tasks assigned to the node. Assigned tasks are then dispatched to the engine.
The agent notifies the _dispatcher_ of the current state of assigned tasks.

This is roughly analagous to an entertainment agent. It ensures the worker has
the correct set of work and let's others know what the worker is doing.

While we refer to cluster engines as a "worker", the term _agent_ encompasses
only the component of a worker that communicates with the dispatcher.

## Objects

An _Object_ is any configuration component accessed as a top-level component.
These typically include a set of API to introspect objects and manipulate them
through a _Spec_. 

_Objects_ are typically broken up into a _Spec_ component and a set of fields
to keep track of the implementation of the Spec. The _Spec_ represents the
users intent. When a user wants to modify an object, only the Spec portion is
provided. When an object flows through the system, the Spec portion is left
untouched by all cluster components.

Examples of objects include _Service_, _Task_, _Network_ and _Volume_.

### Service

The _Service_ instructs the cluster on what needs to be run. It is the central
structure of the cluster system and the primary root of user interaction. The
service informs the orchestrator about how to create and manage tasks.

A _Service_ is configured and updated with changes to `ServiceSpec`. The
central structure of the spec is a `RuntimeSpec`, consisting of definitions on
how to run a container, including attachments to volumes and networks.

### Task

A _Task_ represents a unit of work assigned to a node. A task carries a runtime
definition, describing how to run the container.

As a task flows through the system, its state is updated accordingly. The state
of a task only increases monotonically, meaning that once the task has failed,
it must be recreated to retry.

The assignment of a task to a node is immutable. Once a the task is bound to a
node, it can only run on that node or fail.

### Volume
### Network

