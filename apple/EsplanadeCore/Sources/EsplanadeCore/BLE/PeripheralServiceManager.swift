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

    public weak var delegate: PeripheralServiceManagerDelegate?
    public private(set) var services: [CBMutableService] = []

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

    /// Starts advertising local services.
    /// - Parameters:
    ///   - localName: Optional local name of the peripheral to advertise.
    ///   - serviceUUIDs: Optional array of service UUIDs to advertise.
    public func startAdvertising(_ localName: String?, with serviceUUIDs: [CBUUID]) {
        let currentPeripheralState = self.peripheralManager.state.description

        guard !peripheralManager.isAdvertising else {
            logger.error("Cannot start advertising: Peripheral is already advertising")
            return
        }

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

    public func shutdown() {
        Task { @MainActor in
            if self.peripheralManager.isAdvertising { self.stopAdvertising() }

            let serviceUUIDsToClear = self.services.map { $0.uuid }
            for service in self.services { self.peripheralManager.remove(service) }

            self.services.removeAll()
            // self.serviceImplementations.removeAll() - only applicable in the future, TBC

            self.logger.info("Peripheral manager has been shutdown")
        }
    }

    public nonisolated func peripheralManagerDidUpdateState(_ peripheral: CBPeripheralManager) {
        Task { @MainActor in
            self.logger.info("Peripheral state updated: \(String(describing: peripheral.state))")
            self.delegate?.peripheralServiceManager(self, didUpdateState: peripheral.state)
        }
    }
}

// MARK: - Conformance to `PeripheralServiceManagerDelegate`

@MainActor
public protocol PeripheralServiceManagerDelegate: AnyObject {
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, didUpdateState state: CBManagerState)
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, didStartAdvertising error: Error?)
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, didAdd service: CBService, error: Error?)
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, willRestoreState dict: [String: Any])
}

extension PeripheralServiceManager: CBPeripheralManagerDelegate {
    public nonisolated func peripheralManagerDidStartAdvertising(
        _ peripheral: CBPeripheralManager, error: Error?
    ) {
        Task { @MainActor in
            if let error = error {
                self.logger.error("Failed to start advertising: \(error.localizedDescription)")
            } else {
                self.logger.info("Peripheral manager successfully started advertising.")
            }
            self.delegate?.peripheralServiceManager(self, didStartAdvertising: error)
        }
    }

    public nonisolated func peripheralManager(
        _ peripheral: CBPeripheralManager, didAdd service: CBService, error: Error?
    ) {
        Task { @MainActor in
            if let error = error {
                self.logger.error(
                    "Failed to add service \(service.uuid.uuidString): \(error.localizedDescription)"
                )
            } else {
                self.logger.info("Successfully added service \(service.uuid.uuidString).")
            }
            self.delegate?.peripheralServiceManager(self, didAdd: service, error: error)
        }
    }

    public nonisolated func peripheralManager(
        _ peripheral: CBPeripheralManager, willRestoreState opts: [String: Any]
    ) {
        Task { @MainActor in
            if let restoredServices = opts[CBPeripheralManagerRestoredStateServicesKey]
                as? [CBMutableService]
            {
                self.services = restoredServices
                self.logger.info("Restored \(restoredServices.count) services.")
            }

            self.delegate?.peripheralServiceManager(self, willRestoreState: opts)
        }
    }
}

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
