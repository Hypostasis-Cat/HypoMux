export function DefaultCharacter() {
  return <svg className="ai-pet-character" viewBox="0 0 100 100" fill="none" aria-hidden="true">
        <ellipse className="ai-pet-shadow" cx="50" cy="87" rx="24" ry="5" />
        <g className="ai-pet-body">
          <path className="ai-pet-ear" d="M24 40 23 17Q23 11 29 15L43 30M76 40 77 17Q77 11 71 15L57 30" />
          <path className="ai-pet-shell" d="M17 51C17 30 32 23 50 23S83 30 83 51V59C83 77 68 83 50 83S17 77 17 59Z" />
          <path className="ai-pet-shine" d="M27 40Q34 30 48 31" />
          <rect className="ai-pet-face" x="25" y="42" width="50" height="27" rx="13.5" />
          <g className="ai-pet-eyes"><path d="M39 51V57M61 51V57" /></g>
          <path className="ai-pet-mouth" d="M46 61Q50 65 54 61" />
          <circle className="ai-pet-node" cx="50" cy="32" r="3" />
        </g>
      </svg>;
}
