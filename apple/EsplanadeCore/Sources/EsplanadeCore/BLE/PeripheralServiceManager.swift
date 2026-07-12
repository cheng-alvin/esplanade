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

    public weak var delegate: PeripheralServiceManagerDelegate?

    public var bluetoothState: CBManagerState { peripheralManager.state }
    public var isAdvertising: Bool { peripheralManager.isAdvertising }

    // --- The `CBPeripheralManager` instance! ---
    private var peripheralManager: CBPeripheralManager!
    // --------------------------------------------

    private let logger = Logger(
        subsystem: "com.cheng-alvin.EsplanadeCore", category: "BLEPeripheral")

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
        if serviceUUIDs.isEmpty {
            logger.error("Cannot start advertising: No service UUIDs provided.")
            return
        }
        
        guard peripheralManager.state == .poweredOn else {
            logger.error(
                "Cannot start advertising: Bluetooth state is `\(self.peripheralManager.state.description)`"
            )
            return
        }

        var adData: [String: Any] = [:]
        if let name = localName { adData[CBAdvertisementDataLocalNameKey] = name }
        adData[CBAdvertisementDataServiceUUIDsKey] = serviceUUIDs

        logger.info("Starting advertisement...")
        peripheralManager.startAdvertising(adData)
    }

    public func stopAdvertising() {
        if peripheralManager.isAdvertising {
            logger.info("Stopping advertisement.")
            peripheralManager.stopAdvertising()
        }
    }

    public func add(service: CBMutableService) {
        logger.info("Adding service \(service.uuid.uuidString)")
        peripheralManager.add(service)
    }

    public func remove(service: CBMutableService) {
        logger.info("Removing service \(service.uuid.uuidString)")
        peripheralManager.remove(service)
    }
}

// MARK: - PeripheralServiceManagerDelegate Protocol

@MainActor
public protocol PeripheralServiceManagerDelegate: AnyObject {
    /// Called when the underlying CoreBluetooth peripheral manager updates its hardware power state.
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, didUpdateState state: CBManagerState)

    /// Called when the peripheral manager finishes attempting to start advertising.
    /// - Parameter error: An error if advertising failed to start, or `nil` on success.
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, didStartAdvertising error: Error?)

    /// Called when a service is successfully published/added to the local GATT database.
    /// - Parameters:
    ///   - service: The service that was added.
    ///   - error: An error if publishing the service failed, or `nil` on success.
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, didAdd service: CBService, error: Error?)

    /// Called when the system is restoring the peripheral manager's state after background termination.
    /// - Parameter dict: A dictionary containing preserved state information, such as active services or advertisement data.
    func peripheralServiceManager(
        _ manager: PeripheralServiceManager, willRestoreState dict: [String: Any])
}

extension PeripheralServiceManager: CBPeripheralManagerDelegate {
    public nonisolated func peripheralManagerDidUpdateState(_ peripheral: CBPeripheralManager) {
        Task { @MainActor in
            self.logger.info("Peripheral state updated: \(String(describing: peripheral.state))")
            self.delegate?.peripheralServiceManager(self, didUpdateState: peripheral.state)
        }
    }

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
                self.logger.info(
                    "Successfully added service \(service.uuid.uuidString) to GATT database.")
            }
            self.delegate?.peripheralServiceManager(self, didAdd: service, error: error)
        }
    }

    public nonisolated func peripheralManager(
        _ peripheral: CBPeripheralManager, willRestoreState dict: [String: Any]
    ) {
        Task { @MainActor in
            self.logger.info("Peripheral state is being restored by the system.")
            self.delegate?.peripheralServiceManager(self, willRestoreState: dict)
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
