# Sidebar focus & marker — UX request

Two related requests around sidebar navigation and focus visibility. Filed
2026-05-12 from in-use feedback.

## 1. Cmd-K: focus over toggle

**Current**: cmd-k toggles sidebar visibility (open/close).

**Friction**: after opening a markdown doc from the sidebar, returning to the
sidebar takes two steps — esc or cmd-k to close, then cmd-k to re-open.
Conceptually this is one action ("go back to sidebar"), not two.

**Request**: cmd-k should *focus* the sidebar (showing it if hidden) rather
than toggle visibility. Behavior matrix:

| State                              | cmd-k                          | esc                |
| ---------------------------------- | ------------------------------ | ------------------ |
| sidebar hidden                     | show + focus sidebar           | (no change)        |
| sidebar visible, content focused   | focus sidebar (no visibility change) | close sidebar |
| sidebar visible, sidebar focused   | no-op (or close — open question) | close sidebar    |

Esc as the close affordance is fine.

## 2. Focus marker visibility

**Current**: when sidebar has focus vs. page content has focus, the visual
indicator is hard to read at a glance — requires checking caret position or
similar.

**Request**: make the sidebar's focus state visually obvious. Specifically,
differentiate the sidebar's top divider from the page-vs-header divider when
the sidebar has focus. Options (any subset):

- color: accent color on the divider when sidebar focused
- weight: bolder / thicker line when sidebar focused
- both

Subtle is fine — needs to be parseable at a glance, not screaming.

### Open

- Should the content pane gain a symmetric focus marker? Symmetry would help
  consistency, but could become visual clutter. Defer until #1 is in and the
  workflow shows whether it's needed.

## Why these go together

Request #1 makes cmd-k navigationally cheap. That cheapness is only
worthwhile if the user can immediately tell *whether they're now in the
sidebar* — request #2 closes that loop. Either request alone helps less than
both.
