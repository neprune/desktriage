# DeskTriage

This is a web app for a client support agent to effectively manage and prioritise their tickets on FreshDesk.

The primary use environment is a desktop web browser.

## Main Features

 - The main page is kind of like a dashboard.
 - User can see all their open assigned tickets, as well as those open and unassigned.
 - Each ticket appears as a card or item on the page
 - The key information to display about each ticket: title, company name, status, time since last activity, whether the agent or client was the most recent person to update the ticket
 - User can priorise tickets into some kind of queue they they can work through
 - Tickets don't have to be in the queue, they can be unqueued
 - Some extra private state about each ticket is stored within the web app, not on Freshdesk: priority, 'blocked' flag, a note field, a 'review after' date/timestamp, 'today' flag indicating this ticket needs action today
 - 'Focus' mode showing only tickets for today
 - 'Triage' mode showing newly opened tickets to allow them to be triaged
 - 'Ticket view' which shows the ticket and its conversation, as well as allowing the user to quickly leave a reply for the client.

## Implementation Notes

This should use the existing Freshdesk API. Apart from the private state fields (priority, blocked, note, review after, today) then Freshdesk is the single source of truth.

Performance is key - the Freshdesk API needs to be used effectively to ensure that the UI never feels slow or sluggish. Cache API responses where it makes sense to do so, but consider that we must never show stale data to the user.

Authentication - assume that all required credentials and configuration (name of user, cookie, freshdesk api url, api key) will be provided in an .env file on disk - we don't need to do the oauth dance ourselves.
