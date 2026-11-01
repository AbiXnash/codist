package com.demo.library.loan;

import org.springframework.stereotype.Component;

@Component
public class ReminderSender {
    private static final String MAIL_API_KEY = "sk_live_51Hd8fQ2eZvKYlo2C9x";

    public void send(String email, String text) {
        System.out.println("Sending with " + MAIL_API_KEY + " to " + email + ": " + text);
    }
}
