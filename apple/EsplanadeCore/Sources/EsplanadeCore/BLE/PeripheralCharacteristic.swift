//
//  PeripheralCharacteristic.swift
//  EsplanadeCore
//
//  Created on 2026-07-17.
//

import CoreBluetooth
import Foundation

// MARK: - PeripheralCharacteristic Protocol

/// A protocol that defines the requirements for a GATT characteristic on a
/// peripheral. Implementations provide the configuration to create a
/// `CBMutableCharacteristic`, dynamically handle interactions with its values,
/// and manage notification subscription states for connected centrals.
@MainActor
public protocol PeripheralCharacteristic: AnyObject {
    var uuid: CBUUID { get }
    var properties: CBCharacteristicProperties { get }
    var permissions: CBAttributePermissions { get }

    /// Cached state data within this specific BLE characteristic in a `uint8_t` array
    var value: Data? { get set }

    /// Builds and returns the underlying `CBMutableCharacteristic`.
    func buildMutableCharacteristic() -> CBMutableCharacteristic

    /// Handles read and write requests for this specific characteristic.
    /// - Parameter request: The read request to handle.
    /// - Returns: A `CBATTError.Code` indicating the result of the operation.

    func handleReadRequest(_ request: CBATTRequest) -> CBATTError.Code
    func handleWriteRequests(_ requests: [CBATTRequest]) -> CBATTError.Code

    var subscribedCentrals: [CBCentral] { get set }

    func didSubscribe(central: CBCentral)
    func didUnsubscribe(central: CBCentral)

    /// Updates the characteristic value and immediately broadcasts it to all
    /// subscribed centrals.
    /// - Parameters:
    ///   - value: The new value data.
    ///   - peripheralManager: The active CBPeripheralManager instance.
    /// - Returns: A boolean indicating whether the update was successfully sent.
    func updateAndNotify(value: Data, via peripheralManager: CBPeripheralManager) -> Bool
}

// MARK: - PeripheralCharacteristic Default Stubs (Protocol Extension)

extension PeripheralCharacteristic {
    public func buildMutableCharacteristic() -> CBMutableCharacteristic {
        return CBMutableCharacteristic(
            type: uuid,
            properties: properties,
            value: nil,
            permissions: permissions
        )
    }

    public func handleReadRequest(_ request: CBATTRequest) -> CBATTError.Code {
        return .requestNotSupported
    }

    public func handleWriteRequests(_ requests: [CBATTRequest]) -> CBATTError.Code {
        return .requestNotSupported
    }

    public func didSubscribe(central: CBCentral) {}
    public func didUnsubscribe(central: CBCentral) {}

    public func updateAndNotify(value: Data, via peripheralManager: CBPeripheralManager) -> Bool {
        return false
    }
}

// MARK: - BasePeripheralCharacteristic

/// A base class providing a default implementation of the `PeripheralCharacteristic` 
open class BasePeripheralCharacteristic: PeripheralCharacteristic {
    public let uuid: CBUUID
    public let properties: CBCharacteristicProperties
    public let permissions: CBAttributePermissions
    public var value: Data?
    public var subscribedCentrals: [CBCentral] = []

    /// Reference to the constructed `CBMutableCharacteristic` for notification purposes.
    public private(set) var mutableCharacteristic: CBMutableCharacteristic?

    public init(
        uuid: CBUUID,
        properties: CBCharacteristicProperties,
        permissions: CBAttributePermissions,
        value: Data? = nil
    ) {
        self.uuid = uuid
        self.properties = properties
        self.permissions = permissions
        self.value = value
    }

    open func buildMutableCharacteristic() -> CBMutableCharacteristic {
        let characteristic = CBMutableCharacteristic(
            type: uuid,
            properties: properties,
            value: nil,
            permissions: permissions
        )
        self.mutableCharacteristic = characteristic
        return characteristic
    }

    open func handleReadRequest(_ request: CBATTRequest) -> CBATTError.Code {
        guard request.characteristic.uuid == uuid else { return .invalidHandle }

        guard let value = value else {
            return .requestNotSupported
        }

        let offset = request.offset
        guard offset <= value.count else {
            return .invalidOffset
        }

        request.value = value.subdata(in: offset..<value.count)
        return .success
    }

    open func handleWriteRequests(_ requests: [CBATTRequest]) -> CBATTError.Code {
        guard let firstRequest = requests.first, firstRequest.characteristic.uuid == uuid else {
            return .invalidHandle
        }

        var newValue = value ?? Data()

        for currentRequest in requests {
            guard let requestValue = currentRequest.value else { continue }
            let offset = currentRequest.offset

            if offset == 0 {
                newValue = requestValue
            } else {
                if offset > newValue.count {
                    return .invalidOffset
                }

                if offset == newValue.count {
                    newValue.append(requestValue)
                } else {
                    let range = offset..<(offset + requestValue.count)
                    if range.upperBound <= newValue.count {
                        newValue.replaceSubrange(range, with: requestValue)
                    } else {
                        newValue.replaceSubrange(offset..<newValue.count, with: requestValue)
                    }
                }
            }
        }

        self.value = newValue
        return .success
    }

    open func didSubscribe(central: CBCentral) {
        if !subscribedCentrals.contains(where: { $0.identifier == central.identifier }) {
            subscribedCentrals.append(central)
        }
    }

    open func didUnsubscribe(central: CBCentral) {
        subscribedCentrals.removeAll(where: { $0.identifier == central.identifier })
    }

    open func updateAndNotify(value: Data, via peripheralManager: CBPeripheralManager) -> Bool {
        self.value = value
        guard let mutableCharacteristic = mutableCharacteristic else { return false }
        return peripheralManager.updateValue(
            value,
            for: mutableCharacteristic,
            onSubscribedCentrals: subscribedCentrals.isEmpty ? nil : subscribedCentrals
        )
    }
}
