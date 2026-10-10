import { describe, expect, it } from "vitest";
import { validDNSAddress, validDoHAddress, validDoTAddress } from "./dnsSettings";

describe("DNS address validation", () => {
  it.each(["223.5.5.5", "2001:4860:4860::8888", "::1", "::ffff:1.1.1.1"])("accepts %s", address => expect(validDNSAddress(address)).toBe(true));
  it.each(["", "0.0.0.0", "::", "224.0.0.1", "ff02::1", "fe80::1", "169.254.0.1", "1.1.1.999", "01.1.1.1", "example.com", "1.1.1.1:53"])("rejects %s", address => expect(validDNSAddress(address)).toBe(false));
});

describe("custom DoT validation", () => {
  it.each(["tls://dns.alidns.com", "tls://dns.example.com:8853", "tls://1.1.1.1", "tls://[2001:4860:4860::8888]:853"])("accepts %s", address => expect(validDoTAddress(address)).toBe(true));
  it.each(["", "https://dns.example.com", "tls://dns.example.com/", "tls://dns.example.com?", "tls://dns.example.com#", "tls://user@dns.example.com", "tls://dns.example.com:99999", "tls://dns.example.com:", "tls://0.0.0.0", "tls://[fe80::1]", "tls://dns.example.com:0", "tls://127.1", "tls://☃.example.com"])("rejects %s", address => expect(validDoTAddress(address)).toBe(false));
});

describe("custom DoH validation", () => {
  it.each(["https://dns.example.com/dns-query", "https://dns.example.com:8443/custom/path?key=hello", "https://[2001:4860:4860::8888]/dns-query"])("accepts %s", address => expect(validDoHAddress(address)).toBe(true));
  it.each(["", "http://dns.example.com/dns-query", "https://user:pass@dns.example.com/dns-query", "https://dns.example.com/#fragment", "https://dns.example.com:99999/", "https://dns.example.com:/", "https://0.0.0.0/dns-query", "https://dns.example.com:0/query", "https://127.1/query", "https://☃.example.com/query"])("rejects %s", address => expect(validDoHAddress(address)).toBe(false));
});
