// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, expect, it } from "vitest";
import { VirtualRows } from "./VirtualRows";

afterEach(cleanup);
function List({ count }: { count: number }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  return <div data-testid="scroll" ref={scrollRef}>
    <VirtualRows scrollRef={scrollRef} rows={Array.from({ length: count }, (_, index) => ({
      key: String(index), estimate: 72, render: () => <article>Row {index}</article>,
    }))} />
  </div>;
}
it("renders a bounded window for 1000 rows and reaches the last row on scroll", async () => {
  const view = render(<List count={1000} />);
  expect(screen.getAllByRole("article").length).toBeLessThan(30);
  expect(screen.queryByText("Row 999")).toBeNull();
  const scroller = screen.getByTestId("scroll");
  scroller.scrollTop = 71400;
  fireEvent.scroll(scroller);
  await waitFor(() => expect(screen.getByText("Row 999")).toBeTruthy());
  expect(screen.queryByText("Row 0")).toBeNull();
  expect(screen.getAllByRole("article").length).toBeLessThan(30);
  // Filtering while at the bottom must not leave an empty window.
  view.rerender(<List count={100} />);
  expect(screen.getByText("Row 99")).toBeTruthy();
  view.rerender(<List count={3} />);
  expect(screen.getAllByRole("article")).toHaveLength(3);
});
