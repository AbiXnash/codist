package com.demo.library.book;

import org.springframework.stereotype.Component;

@Component
public class TitleFormatter {
    private static final int MAX_LENGTH = 40;

    /** A title short enough for a receipt line. */
    public String forReceipt(String title) {
        if (title == null || title.isBlank()) {
            return "(untitled)";
        }
        String clean = title.strip();
        if (clean.length() <= MAX_LENGTH) {
            return clean;
        }
        return clean.substring(0, MAX_LENGTH - 1) + "…";
    }
}
