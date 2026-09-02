import Foundation
import Testing

@testable import Lurker

struct EndpointPolicyTests {
  @Test
  func `accepts private destinations`() throws {
    #expect(
      try EndpointPolicy.normalize("http://localhost:8080").absoluteString
        == "http://localhost:8080"
    )
    #expect(try EndpointPolicy.normalize("http://100.64.12.9:8080/").host == "100.64.12.9")
    #expect(try EndpointPolicy.normalize("http://[::1]:8080").absoluteString == "http://[::1]:8080")
    #expect(try EndpointPolicy.normalize("http://lurker").host == "lurker")
    #expect(
      try EndpointPolicy.normalize("http://lurker.example-tailnet.ts.net").host
        == "lurker.example-tailnet.ts.net"
    )
  }

  @Test
  func `rejects public and malformed destinations`() {
    #expect(throws: EndpointPolicyError.insecureLocation) {
      try EndpointPolicy.normalize("http://example.com")
    }
    #expect(throws: EndpointPolicyError.invalidURL) {
      try EndpointPolicy.normalize("https://lurker.example.ts.net")
    }
    #expect(throws: EndpointPolicyError.unsupportedComponents) {
      try EndpointPolicy.normalize("http://localhost:8080/api/state")
    }
    #expect(throws: EndpointPolicyError.unsupportedComponents) {
      try EndpointPolicy.normalize("http://user:password@localhost:8080")
    }
  }

  @Test
  func `enforces tailnet range`() {
    #expect(EndpointPolicy.isPrivateHost("100.64.0.0"))
    #expect(EndpointPolicy.isPrivateHost("100.127.255.255"))
    #expect(!EndpointPolicy.isPrivateHost("100.63.255.255"))
    #expect(!EndpointPolicy.isPrivateHost("100.128.0.0"))
    #expect(EndpointPolicy.isPrivateHost("127.0.0.42"))
  }
}
