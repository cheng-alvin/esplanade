//
//  PeripheralService.swift
//  EsplanadeCore
//
//  Created on 2026-07-17.
//

import CoreBluetooth
import Foundation

// MARK: - PeripheralService Protocol

/// A protocol that defines the requirements for a GATT service on a
/// peripheral. Implementations provide the configuration to create a
/// `CBMutableService` and handle interactions with its characteristics.
@MainActor
public protocol PeripheralServiceProtocol: AnyObject {
    var uuid: CBUUID { get }
    var characteristics: [CBMutableCharacteristic] { get }
    var isPrimary: Bool { get }

    func buildMutableService() -> CBMutableService

    func didSubscribe(to characteristic: CBCharacteristic, central: CBCentral)
    func didUnsubscribe(from characteristic: CBCharacteristic, central: CBCentral)A

    /// Handles read and write requests for this specific characteristic.
    /// - Parameter request: The read request to handle.
    /// - Returns: A `CBATTError.Code` indicating the result of the operation.

    func handleReadRequest(_ request: CBATTRequest) -> CBATTError.Code
    func handleWriteRequests(_ requests: [CBATTRequest]) -> CBATTError.Code
}

// MARK: - PeripheralServiceProtocol Default Stubs (Protocol Extension)

extension PeripheralServiceProtocol {
    public func buildMutableService() -> CBMutableService {
        let service = CBMutableService(type: uuid, primary: isPrimary)
        service.characteristics = characteristics
        return service
    }

    public func handleReadRequest(_ request: CBATTRequest) -> CBATTError.Code {
        return .requestNotSupported
    }

    public func handleWriteRequests(_ requests: [CBATTRequest]) -> CBATTError.Code {
        return .requestNotSupported
    }

    public func didSubscribe(to characteristic: CBCharacteristic, central: CBCentral) {}
    public func didUnsubscribe(from characteristic: CBCharacteristic, central: CBCentral) {}
}

// MARK: - PeripheralService

/// A base class providing a default implementation of the `PeripheralServiceProtocol`.
/// Susbsequent characteristics will conform to `PeripheralCharacteristic`, with all
/// incoming read, write, and notification requests automatically routed to them.
open class PeripheralService: PeripheralServiceProtocol {
    public let uuid: CBUUID
    public let isPrimary: Bool
    public var characteristics: [CBMutableCharacteristic] = []

    public var peripheralCharacteristics: [any PeripheralCharacteristic] = [] {
        didSet {
            self.characteristics = peripheralCharacteristics.map {
                $0.buildMutableserviceCharacteristic()
            }
        }
    }

    public init(uuid: CBUUID, isPrimary: Bool = true) {
        self.uuid = uuid
        self.isPrimary = isPrimary
    }

    public init(
        uuid: CBUUID,
        isPrimary: Bool = true,
        characteristics: [any PeripheralCharacteristic]
    ) {
        self.characteristics = characteristics.map { $0.buildMutableserviceCharacteristic() }

        self.uuid = uuid
        self.isPrimary = isPrimary
        self.peripheralCharacteristics = characteristics
    }

    // MARK: - Internal Helper funciton

    private func serviceCharacteristic(_ uuid: CBUUID) -> (any PeripheralCharacteristic)? {
        return peripheralCharacteristics.first(where: { $0.uuid == uuid })
    }

    open func handleReadRequest(_ request: CBATTRequest) -> CBATTError.Code {
        return serviceCharacteristic(request.characteristic.uuid)?
        .handleReadRequest(request) ?? .requestNotSupported
    }

    open func handleWriteRequests(_ requests: [CBATTRequest]) -> CBATTError.Code {
        guard let firstRequest = requests.first else {
            return .requestNotSupported
        }

        return serviceCharacteristic(firstRequest.characteristic.uuid)?
            .handleWriteRequests(requests) ?? .requestNotSupported
    }

    open func didSubscribe(to characteristic: CBCharacteristic, central: CBCentral) {
        serviceCharacteristic(characteristic.uuid)?.didSubscribe(central: central)
    }

    open func didUnsubscribe(from characteristic: CBCharacteristic, central: CBCentral) {
        serviceCharacteristic(characteristic.uuid)?.didUnsubscribe(central: central)
    }
}
