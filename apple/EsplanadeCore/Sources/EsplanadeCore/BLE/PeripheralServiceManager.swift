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
            logger.error("Cannot start advertising: BLE is currently `\(currentPeripheralState)`")
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

@MainActor
public protocol PeripheralServiceManagerDelegate: AnyObject {
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, didUpdateState state: CBManagerState)
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, didStartAdvertising error: Error?)
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, didAdd service: CBService, error: Error?)

    /// Called when the peripheral manager is restoring its state (e.g., after re-launching
    /// or if a restore identifier was used). This method allows us to rebuild our
    /// service list using the dictionary provided by the system.
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, willRestoreState dict: [String: Any])
}

// MARK: - CBPeripheralManagerDelegate Conformance

extension PeripheralServiceManager: CBPeripheralManagerDelegate {
    public nonisolated func peripheralManagerDidUpdateState(_ peripheral: CBPeripheralManager) {
        Task { @MainActor in self.handleDidUpdateState(peripheral) }
    }

    public nonisolated func peripheralManagerDidStartAdvertising(
        _ peripheral: CBPeripheralManager, error: Error?
    ) {
        Task { @MainActor in self.handleDidStartAdvertising(peripheral, error: error) }
    }

    public nonisolated func peripheralManager(
        _ peripheral: CBPeripheralManager, didAdd service: CBService, error: Error?
    ) {
        Task { @MainActor in self.handleDidAdd(service, error: error) }
    }

    public nonisolated func peripheralManager(
        _ peripheral: CBPeripheralManager, willRestoreState opts: [String: Any]
    ) {
        Task { @MainActor in self.handleWillRestoreState(opts) }
    }

    public nonisolated func peripheralManager(
        _ peripheral: CBPeripheralManager, didReceiveRead request: CBATTRequest
    ) {
        Task { @MainActor in self.handleDidReceiveRead(request) }
    }

    public nonisolated func peripheralManager(
        _ peripheral: CBPeripheralManager, didReceiveWrite requests: [CBATTRequest]
    ) {
        Task { @MainActor in self.handleDidReceiveWrite(requests) }
    }

    public nonisolated func peripheralManager(
        _ peripheral: CBPeripheralManager, central: CBCentral,
        didSubscribeTo characteristic: CBCharacteristic
    ) {
        Task { @MainActor in
            self.handleDidSubscribe(to: characteristic, central: central)
        }
    }

    public nonisolated func peripheralManager(
        _ peripheral: CBPeripheralManager, central: CBCentral,
        didUnsubscribeFrom characteristic: CBCharacteristic
    ) {
        Task { @MainActor in
            self.handleDidUnsubscribe(from: characteristic, central: central)
        }
    }
}

// MARK: - CBPeripheralManager Handling Helpers

extension PeripheralServiceManager {
    fileprivate func handleDidUpdateState(_ peripheral: CBPeripheralManager) {
        logger.info("Peripheral state updated: \(String(describing: peripheral.state))")
        delegate?.peripheralServiceManager(self, didUpdateState: peripheral.state)
    }

    fileprivate func handleDidStartAdvertising(_ peripheral: CBPeripheralManager, error: Error?) {
        if let error = error {
            logger.error("Failed to start advertising: \(error.localizedDescription)")
        } else {
            logger.info("Peripheral manager successfully started advertising.")
        }
        delegate?.peripheralServiceManager(self, didStartAdvertising: error)
    }

    fileprivate func handleDidAdd(_ service: CBService, error: Error?) {
        if let error = error {
            logger.error(
                "Failed to add service \(service.uuid.uuidString): \(error.localizedDescription)"
            )
        } else {
            logger.info("Successfully added service \(service.uuid.uuidString).")
        }
        delegate?.peripheralServiceManager(self, didAdd: service, error: error)
    }

    fileprivate func handleWillRestoreState(_ opts: [String: Any]) {
        if let restoredServices = opts[CBPeripheralManagerRestoredStateServicesKey]
            as? [CBMutableService]
        {
            services = restoredServices
            logger.info("Restored \(restoredServices.count) services.")
        }

        delegate?.peripheralServiceManager(self, willRestoreState: opts)
    }

    fileprivate func handleDidReceiveRead(_ request: CBATTRequest) {
        guard let serviceUUID = request.characteristic.service?.uuid,
            let implementation = serviceImplementations[serviceUUID]
        else {
            let serviceName = request.characteristic.service?.uuid.uuidString ?? "(unknown)"
            logger.error("Service \(serviceName) does not exist")
            peripheralManager.respond(to: request, withResult: .attributeNotFound)

            return
        }

        let result = implementation.handleReadRequest(request)
        peripheralManager.respond(to: request, withResult: result)
    }

    fileprivate func handleDidReceiveWrite(_ requests: [CBATTRequest]) {
        guard let firstRequest = requests.first,
            let serviceUUID = firstRequest.characteristic.service?.uuid,
            let implementation = serviceImplementations[serviceUUID]
        else {
            logger.warning("No implementation found for write requests.")
            if let first = requests.first {
                peripheralManager.respond(to: first, withResult: .attributeNotFound)
            }

            return
        }

        let result = implementation.handleWriteRequests(requests)
        peripheralManager.respond(to: firstRequest, withResult: result)
    }

    fileprivate func handleDidSubscribe(to characteristic: CBCharacteristic, central: CBCentral) {
        guard let serviceUUID = characteristic.service?.uuid,
            let implementation = serviceImplementations[serviceUUID]
        else {
            logger.error("\(characteristic.uuid.uuidString) not found")
            return
        }

        logger.info(
            "\(central.identifier.uuidString) subscribed to \(characteristic.uuid.uuidString)")
        implementation.didSubscribe(to: characteristic, central: central)
    }

    fileprivate func handleDidUnsubscribe(from characteristic: CBCharacteristic, central: CBCentral)
    {
        guard let serviceUUID = characteristic.service?.uuid,
            let implementation = serviceImplementations[serviceUUID]
        else {
            logger.error("\(characteristic.uuid.uuidString) not found")
            return
        }

        logger.info(
            "\(central.identifier.uuidString) unsubscribed from \(characteristic.uuid.uuidString)")
        implementation.didUnsubscribe(from: characteristic, central: central)
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
