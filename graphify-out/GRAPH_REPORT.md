# Graph Report - esplanade  (2026-08-22)

## Corpus Check
- 28 files · ~84,307 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 159 nodes · 275 edges · 11 communities (8 shown, 3 thin omitted)
- Extraction: 99% EXTRACTED · 1% INFERRED · 0% AMBIGUOUS · INFERRED: 4 edges (avg confidence: 0.84)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `834176f2`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- PeripheralServiceManager
- BasePeripheralCharacteristic
- PeripheralService
- ContentView
- net/http.ResponseWriter
- buildRouter
- PeripheralServiceManager.swift
- Config
- Package.swift
- smoke_test.sh
- esplanade

## God Nodes (most connected - your core abstractions)
1. `PeripheralServiceManager` - 27 edges
2. `BasePeripheralCharacteristic` - 15 edges
3. `PeripheralService` - 13 edges
4. `PeripheralCharacteristic` - 12 edges
5. `PeripheralServiceProtocol` - 11 edges
6. `buildRouter()` - 10 edges
7. `Config` - 7 edges
8. `Logger()` - 6 edges
9. `Server` - 6 edges
10. `New()` - 6 edges

## Surprising Connections (you probably didn't know these)
- `New()` --calls--> `buildRouter()`  [INFERRED]
  server/internal/server/server.go → server/internal/server/routes.go
- `PeripheralService` --references--> `PeripheralCharacteristic`  [EXTRACTED]
  apple/EsplanadeCore/Sources/EsplanadeCore/BLE/PeripheralService.swift → apple/EsplanadeCore/Sources/EsplanadeCore/BLE/PeripheralCharacteristic.swift
- `PeripheralServiceManager` --references--> `PeripheralServiceProtocol`  [EXTRACTED]
  apple/EsplanadeCore/Sources/EsplanadeCore/BLE/PeripheralServiceManager.swift → apple/EsplanadeCore/Sources/EsplanadeCore/BLE/PeripheralService.swift
- `.body` --calls--> `ContentView`  [INFERRED]
  apple/EsplanadeMacOS/EsplanadeMacOSApp.swift → apple/EsplanadeMacOS/ContentView.swift
- `.body` --calls--> `ContentView`  [INFERRED]
  apple/EsplanadeiOS/EsplanadeiOSApp.swift → apple/EsplanadeiOS/ContentView.swift

## Import Cycles
- None detected.

## Communities (11 total, 3 thin omitted)

### Community 0 - "PeripheralServiceManager"
Cohesion: 0.12
Nodes (18): Any, PeripheralServiceManager, .bluetoothState, .isAdvertising, SubscriptionChange, subscribing, unsubscribing, Bool (+10 more)

### Community 1 - "BasePeripheralCharacteristic"
Cohesion: 0.15
Nodes (12): BasePeripheralCharacteristic, PeripheralCharacteristic, Bool, CBATTError, CBATTRequest, CBCentral, CBMutableCharacteristic, CBPeripheralManager (+4 more)

### Community 2 - "PeripheralService"
Cohesion: 0.17
Nodes (11): PeripheralService, .peripheralCharacteristics, PeripheralServiceProtocol, Bool, CBATTError, CBATTRequest, CBCentral, CBCharacteristic (+3 more)

### Community 3 - "ContentView"
Cohesion: 0.15
Nodes (13): App, ContentView, .body, EsplanadeiOSApp, .body, Scene, ContentView, .body (+5 more)

### Community 4 - "net/http.ResponseWriter"
Cohesion: 0.24
Nodes (12): ExampleResponse, Handler, net/http.Request, net/http.ResponseWriter, envelope, New(), Created(), Error() (+4 more)

### Community 5 - "buildRouter"
Cohesion: 0.18
Nodes (10): log/slog.Logger, net/http.Handler, Handler, responseWriter, New(), CORS(), Logger(), wrapResponseWriter() (+2 more)

### Community 6 - "PeripheralServiceManager.swift"
Cohesion: 0.20
Nodes (8): AnyObject, CBManagerState, .description, PeripheralServiceManagerDelegate, CoreBluetooth, CustomStringConvertible, Foundation, os

### Community 7 - "Config"
Cohesion: 0.26
Nodes (8): Env, net/http.Server, time.Duration, main(), Config, Load(), New(), Server

## Knowledge Gaps
- **14 isolated node(s):** `PackageDescription`, `.peripheralCharacteristics`, `os`, `.bluetoothState`, `.isAdvertising` (+9 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **3 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `PeripheralServiceManager` connect `PeripheralServiceManager` to `PeripheralService`, `PeripheralServiceManager.swift`?**
  _High betweenness centrality (0.162) - this node is a cross-community bridge._
- **Why does `PeripheralServiceProtocol` connect `PeripheralService` to `PeripheralServiceManager`, `PeripheralServiceManager.swift`?**
  _High betweenness centrality (0.128) - this node is a cross-community bridge._
- **Why does `PeripheralCharacteristic` connect `BasePeripheralCharacteristic` to `PeripheralService`, `PeripheralServiceManager.swift`?**
  _High betweenness centrality (0.115) - this node is a cross-community bridge._
- **What connects `PackageDescription`, `.peripheralCharacteristics`, `os` to the rest of the system?**
  _14 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `PeripheralServiceManager` be split into smaller, more focused modules?**
  _Cohesion score 0.11612903225806452 - nodes in this community are weakly interconnected._
- **Should `BasePeripheralCharacteristic` be split into smaller, more focused modules?**
  _Cohesion score 0.14666666666666667 - nodes in this community are weakly interconnected._
- **Should `ContentView` be split into smaller, more focused modules?**
  _Cohesion score 0.14705882352941177 - nodes in this community are weakly interconnected._