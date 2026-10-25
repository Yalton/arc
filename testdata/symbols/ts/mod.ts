import { x } from "./other";

/**
 * Parses an import line.
 *
 * @param line the line
 */
export function parseImportLine(line: string): string {
  return line;
}

// Default timeout in ms.
// Second line of the comment.
export const DEFAULT_TIMEOUT = 1000;

export let counter = 0;

/** A bead label. */
export interface BeadLabel {
  name: string;
}

export type LabelName = string;

@decorator
export class LabelStore {}

export default async function* walk() {}

export const enum Color { Red }

const tmpl = `
export function insideTemplate() {}
`;

/*
export function insideComment() {}
*/

function local() {}
