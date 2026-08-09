//
//  PeripheralServiceManager.swift
//  EsplanadeCore
//
//  Created on 2026-07-12.
//

import CoreBluetooth
import Foundation
import os

// MARK: - PeripheralServiceManager

@MainActor
public final class PeripheralServiceManager: NSObject {

    private let logger = Logger(
        subsystem: "com.cheng-alvin.EsplanadeCore", category: "BLEPeripheral")

    private var peripheralManager: CBPeripheralManager!
    private var serviceImplementations: [CBUUID: any PeripheralServiceProtocol] = [:]
    public private(set) var services: [CBMutableService] = []

    public weak var delegate: PeripheralServiceManagerDelegate?

    public var bluetoothState: CBManagerState { peripheralManager.state }
    public var isAdvertising: Bool { peripheralManager.isAdvertising }

    public init(restoreIdentifier: String? = nil) {
        super.init()

        var options: [String: Any] = [:]

        options[CBPeripheralManagerOptionShowPowerAlertKey] = true
        if let restoreId = restoreIdentifier {
            options[CBPeripheralManagerOptionRestoreIdentifierKey] = restoreId as NSString
        }

        self.peripheralManager = CBPeripheralManager(delegate: self, queue: nil, options: options)
        self.logger.info("PeripheralServiceManager initialized.")
    }

    // MARK: - Public API
    public func startAdvertising(_ localName: String?, with serviceUUIDs: [CBUUID]) {
        let currentPeripheralState = self.peripheralManager.state.description

        guard !peripheralManager.isAdvertising else { return }

        if serviceUUIDs.isEmpty {
            logger.error("Cannot start advertising: No service UUIDs provided.")
            return
        }

        guard peripheralManager.state == .poweredOn else {
            logger.error("Cannot start advertising: state is `\(currentPeripheralState)`")
            return
        }

        var adData: [String: Any] = [:]
        if let name = localName { adData[CBAdvertisementDataLocalNameKey] = name }
        adData[CBAdvertisementDataServiceUUIDsKey] = serviceUUIDs

        peripheralManager.startAdvertising(adData)
    }

    public func stopAdvertising() {
        if peripheralManager.isAdvertising {
            logger.info("Stopping advertisement.")
            peripheralManager.stopAdvertising()
        }
    }

    public func add(service peripheralService: any PeripheralServiceProtocol) {
        let mutableService = peripheralService.buildMutableService()

        if !services.contains(where: { $0.uuid == mutableService.uuid }) {
            services.append(mutableService)
            peripheralManager.add(mutableService)
            serviceImplementations[mutableService.uuid] = peripheralService

            logger.info("\(mutableService.uuid.uuidString) added")
        } else {
            logger.warning("Service \(mutableService.uuid.uuidString) already exists!")
        }
    }

    public func remove(service: CBMutableService) {
        services.removeAll { $0.uuid == service.uuid }
        serviceImplementations.removeValue(forKey: service.uuid)
        peripheralManager.remove(service)

        logger.info("Removed \(service.uuid.uuidString)")
    }

    public func cleanup() {
        Task { @MainActor in
            for service in self.services {
                self.peripheralManager.remove(service)
            }

            self.serviceImplementations.removeAll()
            self.services.removeAll()

            self.logger.info("Peripheral manager cleaned up")
        }
    }
}

// MARK: - PeripheralServiceManagerDelegate Protocol

/// A delegate protocol that receives events and updates from `PeripheralServiceManager`,
/// allowing the conforming type to react to state changes, advertising status, and
/// BLE service interactions.
@MainActor
public protocol PeripheralServiceManagerDelegate: AnyObject {
    /// Invoked when the peripheral manager's state updates. added for conformance
    /// to CoreBluetooth's `CBPeripheralManagerDelegate` protocol.
    /// - Parameters:
    ///   - manager: The `PeripheralServiceManager` instance.
    ///   - state: The current state of the `CBPeripheralManager`.
    func peripheralManager(
        _ manager: PeripheralServiceManager, didUpdateState state: CBManagerState)

    func peripheralManager(_ manager: PeripheralServiceManager, didStartAdvertising error: Error?)

    /// Notifies the delegate that the peripheral manager is restoring its state
    /// (e.g., after re-launching or if a restore identifier was then used). This
    /// allows the delegate to rebuild its service list using provided data.
    /// - Parameters:
    ///   - manager: The `PeripheralServiceManager` instance.
    ///   - state: A dictionary containing the restored state.
    func peripheralManager(
        _ manager: PeripheralServiceManager, willRestoreState state: [String: Any])

    /// Called when the manager is ready to send updates to subscribed central devices.
    /// - Parameter manager: The `PeripheralServiceManager` instance.
    func peripheralManagerIsReady(toUpdateSubscribers manager: PeripheralServiceManager)
}

extension PeripheralServiceManagerDelegate {
    public func peripheralManagerIsReady(toUpdateSubscribers manager: PeripheralServiceManager) {}
}

// MARK: - CBPeripheralManagerDelegate Conformance

extension PeripheralServiceManager: @preconcurrency CBPeripheralManagerDelegate {
    /// Required by `CBPeripheralManagerDelegate` to monitor state changes.
    @MainActor
    public func peripheralManagerDidUpdateState(_ peripheral: CBPeripheralManager) {
        logger.info("Peripheral state updated: \(String(describing: peripheral.state))")
        delegate?.peripheralManager(self, didUpdateState: peripheral.state)
    }

    @MainActor
    public func peripheralManagerDidStartAdvertising(
        _ peripheral: CBPeripheralManager, error: Error?
    ) {
        logResult(of: "Advertisement start", error: error)
        delegate?.peripheralManager(self, didStartAdvertising: error)
    }

    @MainActor
    public func peripheralManager(
        _ peripheral: CBPeripheralManager, willRestoreState opts: [String: Any]
    ) {
        if let restoredServices = opts[CBPeripheralManagerRestoredStateServicesKey]
            as? [CBMutableService]
        {
            services = restoredServices
            logger.info("Restored \(restoredServices.count) services.")
        }
        delegate?.peripheralManager(self, willRestoreState: opts)
    }

    @MainActor
    public func peripheralManager(
        _ peripheral: CBPeripheralManager, didReceiveRead request: CBATTRequest
    ) {
        guard let implementation = serviceImplementation(for: request.characteristic) else {
            let serviceName = request.characteristic.service?.uuid.uuidString ?? "(unknown)"
            logger.error("Service \(serviceName) does not exist")
            peripheral.respond(to: request, withResult: .attributeNotFound)
            return
        }

        let result = implementation.handleReadRequest(request)
        peripheral.respond(to: request, withResult: result)
    }

    @MainActor
    public func peripheralManager(
        _ peripheral: CBPeripheralManager, didReceiveWrite requests: [CBATTRequest]
    ) {
        guard let firstRequest = requests.first,
            let implementation = serviceImplementation(for: firstRequest.characteristic)
        else {
            logger.warning("No implementation found for write requests.")
            if let first = requests.first {
                peripheral.respond(to: first, withResult: .attributeNotFound)
            }
            return
        }

        let result = implementation.handleWriteRequests(requests)
        peripheral.respond(to: firstRequest, withResult: result)
    }

    @MainActor
    public func peripheralManager(
        _ peripheral: CBPeripheralManager, central: CBCentral,
        didSubscribeTo characteristic: CBCharacteristic
    ) {
        handleSubscriptionChange(.subscribing, of: central, to: characteristic)
    }

    @MainActor
    public func peripheralManager(
        _ peripheral: CBPeripheralManager, central: CBCentral,
        didUnsubscribeFrom characteristic: CBCharacteristic
    ) {
        handleSubscriptionChange(.unsubscribing, of: central, to: characteristic)
    }

    @MainActor
    public func peripheralManagerIsReady(toUpdateSubscribers peripheral: CBPeripheralManager) {
        logger.info("Peripheral manager is ready to update subscribers.")
        delegate?.peripheralManagerIsReady(toUpdateSubscribers: self)
    }
}

// MARK: - Private Helpers

extension PeripheralServiceManager {
    private func logResult(of operation: String, error: (any Error)?) {
        if let error {
            logger.error("\(operation) failed: \(error.localizedDescription)")
        } else {
            logger.info("\(operation) completed successfully.")
        }
    }

    private func serviceImplementation(for characteristic: CBCharacteristic) -> (
        any PeripheralServiceProtocol
    )? {
        guard let serviceUUID = characteristic.service?.uuid else { return nil }
        return serviceImplementations[serviceUUID]
    }

    private enum SubscriptionChange {
        case subscribing
        case unsubscribing

        func logMessage() -> String {
            switch self {
            case .subscribing: return "subscribed to"
            case .unsubscribing: return "unsubscribed from"
            }
        }

        func logMessage(central: String, characteristic: String) -> String {
            "\(central) \(self.logMessage()) \(characteristic)"
        }
    }

    private func handleSubscriptionChange(
        _ change: SubscriptionChange,
        of central: CBCentral,
        to characteristic: CBCharacteristic
    ) {
        let characteristicID = characteristic.uuid.uuidString
        let centralID = central.identifier.uuidString

        guard let implementation = serviceImplementation(for: characteristic) else {
            logger.error("\(characteristicID) not found")
            return
        }

        logger.info("\(change.logMessage(central: centralID, characteristic: characteristicID))")

        switch change {
        case .subscribing: implementation.didSubscribe(to: characteristic, central: central)
        case .unsubscribing: implementation.didUnsubscribe(from: characteristic, central: central)
        }
    }
}

// MARK: - CBManagerState CustomStringConvertible

extension CBManagerState: @retroactive CustomStringConvertible {
    public var description: String {
        switch self {
        case .unknown: return "Unknown"
        case .resetting: return "Resetting"
        case .unsupported: return "Unsupported"
        case .unauthorized: return "Unauthorized"
        case .poweredOff: return "PoweredOff"
        case .poweredOn: return "PoweredOn"
        @unknown default: return "UnknownFutureState"
        }
    }
}
