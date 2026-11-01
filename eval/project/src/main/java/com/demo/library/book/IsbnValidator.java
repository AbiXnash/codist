package com.demo.library.book;

import org.springframework.stereotype.Component;

@Component
public class IsbnValidator {
    /** ISBN-13: digits weigh 1 and 3 in turn, and the total must divide by 10. */
    public boolean valid(String isbn) {
        String digits = isbn.replace("-", "");
        if (digits.length() != 13) {
            return false;
        }
        int total = 0;
        for (int i = 0; i < digits.length() - 1; i++) {
            int digit = digits.charAt(i) - '0';
            total += i % 2 == 0 ? digit : digit * 3;
        }
        return total % 10 == 0;
    }
}
